// Package ws streams a source's fMP4 segments to a browser over WebSocket.
//
// Protocol (server → client):
//   - text   {"type":"status","state":"live|connecting|...","error":"..."}
//   - text   {"type":"init","codec":"avc1.4D401F"}, followed by one binary
//     init segment. Sent again whenever the source restarts; the player must
//     then reset its decoder.
//   - binary media segments, each starting on a keyframe.
package ws

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/stream"
)

const (
	writeTimeout = 10 * time.Second
	pingInterval = 20 * time.Second // keeps proxies from closing idle sockets
)

type Options struct {
	OriginHosts []string // host[:port] patterns allowed to connect
	AnyOrigin   bool
}

// Handler serves GET /ws/{id}.
func Handler(m *stream.Manager, opts Options) http.HandlerFunc {
	accept := &websocket.AcceptOptions{
		OriginPatterns:     opts.OriginHosts,
		InsecureSkipVerify: opts.AnyOrigin,
		CompressionMode:    websocket.CompressionDisabled, // video doesn't compress
	}
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := m.Get(r.PathValue("id"))
		if !ok {
			http.Error(w, stream.ErrNotFound.Error(), http.StatusNotFound)
			return
		}
		conn, err := websocket.Accept(w, r, accept)
		if err != nil {
			return // Accept already wrote the HTTP error
		}
		defer conn.CloseNow()

		client, ok := s.Subscribe()
		if !ok {
			conn.Close(websocket.StatusGoingAway, "stream removed")
			return
		}
		defer s.Unsubscribe(client)

		// Clients never send data; this handles close frames and pongs.
		ctx := conn.CloseRead(r.Context())
		ping := time.NewTicker(pingInterval)
		defer ping.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-client.C:
				if !ok {
					// Stream removed, server shutting down, or client too slow.
					conn.Close(websocket.StatusGoingAway, "stream closed")
					return
				}
				typ := websocket.MessageBinary
				if msg.Text {
					typ = websocket.MessageText
				}
				if err := write(ctx, conn, typ, msg.Data); err != nil {
					return
				}
			case <-ping.C:
				pctx, cancel := context.WithTimeout(ctx, writeTimeout)
				err := conn.Ping(pctx)
				cancel()
				if err != nil {
					log.Printf("stream %s: viewer ping failed: %v", s.ID(), err)
					return
				}
			}
		}
	}
}

func write(ctx context.Context, conn *websocket.Conn, typ websocket.MessageType, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, typ, data)
}
