package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"

	"github.com/missuo/wsmask/internal/proto"
	"github.com/missuo/wsmask/internal/tunnel"
)

var (
	listenAddr = flag.String("listen", "127.0.0.1:12345", "local TCP listen address")
	serverURL  = flag.String("server", "", "upstream ws://host:port/path (required)")
	fakeHost   = flag.String("fake-host", "www.bing.com", "HTTP Host header to send on WS handshake")
	authToken  = flag.String("auth", "", "shared auth token (required)")
	devTarget  = flag.String("target", "", "(dev mode) skip SO_ORIGINAL_DST, force this host:port as the real target")
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	flag.Parse()

	if *serverURL == "" || *authToken == "" {
		log.Fatal("-server and -auth are required")
	}
	u, err := url.Parse(*serverURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		log.Fatalf("invalid -server %q: must be ws:// or wss:// URL", *serverURL)
	}

	l, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("listen %s: %v", *listenAddr, err)
	}
	defer l.Close()

	log.Printf("=== wsmask-client ===")
	log.Printf("  listen    = %s", *listenAddr)
	log.Printf("  server    = %s", u)
	log.Printf("  fake-host = %s", *fakeHost)
	if *devTarget != "" {
		log.Printf("  target    = %s  (DEV MODE: SO_ORIGINAL_DST bypassed)", *devTarget)
	} else {
		log.Printf("  target    = <SO_ORIGINAL_DST>")
	}

	for {
		c, err := l.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handle(c.(*net.TCPConn), u)
	}
}

func handle(c *net.TCPConn, u *url.URL) {
	defer c.Close()
	tag := c.RemoteAddr().String()
	log.Printf("[%s] accepted (local=%s)", tag, c.LocalAddr())

	var dst string
	if *devTarget != "" {
		dst = *devTarget
		log.Printf("[%s] using dev target %s", tag, dst)
	} else {
		d, err := proto.OriginalDst(c)
		if err != nil {
			log.Printf("[%s] SO_ORIGINAL_DST failed: %v", tag, err)
			return
		}
		dst = d.String()
		log.Printf("[%s] hijacked real dst %s", tag, dst)
	}

	header := http.Header{}
	header.Set("Host", *fakeHost)
	header.Set("X-Target", dst)
	header.Set("X-Auth", *authToken)
	header.Set("User-Agent", "Mozilla/5.0 (compatible; wsmask/0.1)")

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	dialer.EnableCompression = false

	log.Printf("[%s] WS handshake → %s  Host=%s  X-Target=%s", tag, u, *fakeHost, dst)
	t0 := time.Now()
	ws, resp, err := dialer.Dial(u.String(), header)
	if err != nil {
		extra := ""
		if resp != nil {
			extra = fmt.Sprintf(" (HTTP %d)", resp.StatusCode)
		}
		log.Printf("[%s] WS handshake failed after %s%s: %v", tag, time.Since(t0), extra, err)
		return
	}
	defer ws.Close()
	log.Printf("[%s] WS handshake OK in %s (HTTP %d)", tag, time.Since(t0), resp.StatusCode)

	tunnel.Pump(ws, c, tag)
}
