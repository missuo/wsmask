package tunnel

import (
	"io"
	"log"
	"net"

	"github.com/gorilla/websocket"
)

const copyBufSize = 16 * 1024

// Pump runs bidirectional byte copy between a WebSocket conn and a TCP conn.
// Blocks until one side errors, then closes both and waits for the other
// goroutine to drain. tag is used for logging.
func Pump(ws *websocket.Conn, tcp net.Conn, tag string) {
	type result struct {
		dir   string
		bytes int64
		err   error
	}
	errc := make(chan result, 2)

	go func() {
		buf := make([]byte, copyBufSize)
		var total int64
		for {
			_, r, err := ws.NextReader()
			if err != nil {
				errc <- result{"ws→tcp", total, err}
				return
			}
			n, err := io.CopyBuffer(tcp, r, buf)
			total += n
			if err != nil {
				errc <- result{"ws→tcp", total, err}
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, copyBufSize)
		var total int64
		for {
			n, err := tcp.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					errc <- result{"tcp→ws", total, werr}
					return
				}
				total += int64(n)
			}
			if err != nil {
				errc <- result{"tcp→ws", total, err}
				return
			}
		}
	}()

	first := <-errc
	log.Printf("[%s] %s done (%d bytes): %v", tag, first.dir, first.bytes, first.err)

	tcp.Close()
	ws.Close()

	second := <-errc
	log.Printf("[%s] %s done (%d bytes): %v", tag, second.dir, second.bytes, second.err)
	log.Printf("[%s] tunnel closed", tag)
}
