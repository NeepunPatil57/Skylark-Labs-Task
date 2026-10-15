package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/mp4"
	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/stream"
)

// fakeCamera sends an init segment and then a media segment every 10ms.
func fakeCamera(ctx context.Context, _ string, _ stream.RunOptions, onSegment func(mp4.Segment) error) error {
	if err := onSegment(mp4.Segment{Init: true, Codec: "avc1.4D401F", Data: []byte("init")}); err != nil {
		return err
	}
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := onSegment(mp4.Segment{Data: []byte("media")}); err != nil {
				return err
			}
		}
	}
}

func setup(t *testing.T, opts Options) (*stream.Manager, *httptest.Server, string) {
	t.Helper()
	m := stream.NewManager(stream.Options{Runner: fakeCamera, MaxStreams: 4, IdleTimeout: time.Second})
	info, _, err := m.Add("rtsp://cam/test", "")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /ws/{id}", Handler(m, opts))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		m.Close()
		srv.Close()
	})
	return m, srv, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/" + info.ID
}

func TestStreamsStatusInitAndMedia(t *testing.T) {
	_, _, url := setup(t, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	var sawInitJSON, sawInitSegment, sawLive bool
	media := 0
	for media < 3 {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if typ == websocket.MessageText {
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("bad JSON %q", data)
			}
			sawInitJSON = sawInitJSON || (msg["type"] == "init" && msg["codec"] == "avc1.4D401F")
			sawLive = sawLive || (msg["type"] == "status" && msg["state"] == "live")
			continue
		}
		switch string(data) {
		case "init":
			if !sawInitJSON {
				t.Error("init segment arrived before the codec message")
			}
			sawInitSegment = true
		case "media":
			if !sawInitSegment {
				t.Fatal("media arrived before the init segment")
			}
			media++
		}
	}
	if !sawLive {
		t.Error("no live status")
	}
}

func TestUnknownStreamIs404(t *testing.T) {
	_, srv, _ := setup(t, Options{})
	res, err := http.Get(srv.URL + "/ws/doesnotexist")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", res.StatusCode)
	}
}

func TestRemovingStreamClosesSocket(t *testing.T) {
	m, _, url := setup(t, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatal(err)
	}

	id := url[strings.LastIndex(url, "/")+1:]
	if err := m.Remove(id); err != nil {
		t.Fatal(err)
	}
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			if status := websocket.CloseStatus(err); status != websocket.StatusGoingAway {
				t.Errorf("close status = %v (%v), want going away", status, err)
			}
			return
		}
	}
}

func TestRejectsUnlistedOrigin(t *testing.T) {
	_, _, url := setup(t, Options{OriginHosts: []string{"app.example.com"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	header := http.Header{"Origin": {"https://evil.example.com"}}
	if conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header}); err == nil {
		conn.CloseNow()
		t.Fatal("cross-origin connection from an unlisted origin was accepted")
	}

	header.Set("Origin", "https://app.example.com")
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatalf("allowed origin rejected: %v", err)
	}
	conn.CloseNow()
}
