package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/missuo/wsmask/internal/tunnel"
)

var (
	listenAddr = flag.String("listen", ":8080", "HTTP listen address")
	wsPath     = flag.String("path", "/ec-McAuth", "WebSocket upgrade path")
	authToken  = flag.String("auth", "", "shared auth token (required)")
	dialTO     = flag.Duration("dial-timeout", 10*time.Second, "upstream dial timeout")
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:    4096,
	WriteBufferSize:   4096,
	EnableCompression: false,
	CheckOrigin:       func(_ *http.Request) bool { return true },
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	flag.Parse()

	if *authToken == "" {
		log.Fatal("-auth is required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc(*wsPath, handle)
	if *wsPath != "/" {
		mux.HandleFunc("/", decoy)
	}

	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("=== wsmask-server ===")
	log.Printf("  listen = %s", *listenAddr)
	log.Printf("  path   = %s", *wsPath)
	log.Fatal(srv.ListenAndServe())
}

func handle(w http.ResponseWriter, r *http.Request) {
	tag := r.RemoteAddr
	log.Printf("[%s] %s %s Host=%q UA=%q Upgrade=%q",
		tag, r.Method, r.URL.Path, r.Host, r.UserAgent(), r.Header.Get("Upgrade"))

	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		log.Printf("[%s] not a WS upgrade request → decoy", tag)
		decoy(w, r)
		return
	}

	target, ok := parseC(r.URL.Query().Get("c"), *authToken)
	if !ok {
		log.Printf("[%s] invalid or missing ?c= → decoy", tag)
		decoy(w, r)
		return
	}

	log.Printf("[%s] resolving target %s", tag, target)
	remote, err := safeDial(target, *dialTO)
	if err != nil {
		log.Printf("[%s] dial %s failed: %v", tag, target, err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer remote.Close()
	log.Printf("[%s] upstream dialed %s (local=%s)", tag, remote.RemoteAddr(), remote.LocalAddr())

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[%s] WS upgrade failed: %v", tag, err)
		return
	}
	defer ws.Close()

	sessTag := fmt.Sprintf("%s⇄%s", tag, target)
	log.Printf("[%s] tunnel open", sessTag)
	tunnel.Pump(ws, remote, sessTag)
}

// parseC decodes base64url(target|token). Returns target and ok=true only
// if the encoding is well-formed and the token matches.
func parseC(c, expectedToken string) (string, bool) {
	if c == "" {
		return "", false
	}
	raw, err := base64.URLEncoding.DecodeString(c)
	if err != nil {
		return "", false
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return "", false
	}
	target, token := parts[0], parts[1]
	if token != expectedToken || target == "" {
		return "", false
	}
	return target, true
}

func decoy(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Server", "nginx/1.24.0")
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("<html>\r\n<head><title>404 Not Found</title></head>\r\n<body>\r\n<center><h1>404 Not Found</h1></center>\r\n<hr><center>nginx/1.24.0</center>\r\n</body>\r\n</html>\r\n"))
}

func safeDial(target string, timeout time.Duration) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("bad target format: %w", err)
	}
	if port == "" {
		return nil, errors.New("empty port")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP for %s", host)
	}

	var lastErr error
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return nil, fmt.Errorf("blocked IP %s for host %s", ip, host)
		}
		addr := net.JoinHostPort(ip.String(), port)
		log.Printf("        trying %s", addr)
		c, err := net.DialTimeout("tcp", addr, timeout)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
