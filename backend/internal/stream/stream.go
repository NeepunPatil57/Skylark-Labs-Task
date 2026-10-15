package stream

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/hub"
	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/mp4"
)

type State string

const (
	StateIdle         State = "idle"         // no viewers, source not pulled
	StateConnecting   State = "connecting"   // first attempt after going active
	StateLive         State = "live"         // receiving video
	StateReconnecting State = "reconnecting" // transient failure, retrying
	StateError        State = "error"        // likely permanent failure, still retrying slowly
)

const (
	minBackoff    = 1 * time.Second
	maxBackoff    = 30 * time.Second
	clientBufSize = 64 // media segments (~1s each) a viewer may lag behind
)

// Info is the public view of a stream. URL never contains credentials.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	State   State  `json:"state"`
	Error   string `json:"error,omitempty"`
	Codec   string `json:"codec,omitempty"`
	Viewers int    `json:"viewers"`
}

// Stream owns one RTSP source. It pulls the source only while someone is
// watching and shares that single pull with every viewer.
type Stream struct {
	id      string
	name    string
	url     string // may contain credentials: never log or return it
	safeURL string
	runner  Runner
	idle    time.Duration
	wg      *sync.WaitGroup
	hub     *hub.Hub
	kick    chan struct{} // skips the current backoff wait

	mu        sync.Mutex
	state     State
	errMsg    string
	codec     string
	viewers   int
	gen       int // increments per run, so a stopping run can't clobber a newer one
	cancelRun context.CancelFunc
	idleTimer *time.Timer
	removed   bool
}

func (s *Stream) ID() string { return s.id }

func (s *Stream) Info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{
		ID: s.id, Name: s.name, URL: s.safeURL,
		State: s.state, Error: s.errMsg, Codec: s.codec, Viewers: s.viewers,
	}
}

// Subscribe adds a viewer, starting the source if it isn't running. If the
// source is waiting to retry, it retries now. Returns false once the stream
// has been removed.
func (s *Stream) Subscribe() (*hub.Client, bool) {
	c, ok := s.hub.Subscribe()
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.removed {
		s.hub.Unsubscribe(c)
		return nil, false
	}
	s.viewers++
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	if s.cancelRun == nil {
		s.start()
	} else {
		select {
		case s.kick <- struct{}{}:
		default:
		}
	}
	return c, true
}

// Unsubscribe removes a viewer. The source stops after the idle timeout if
// nobody else is watching.
func (s *Stream) Unsubscribe(c *hub.Client) {
	s.hub.Unsubscribe(c)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewers--
	if s.viewers > 0 || s.removed || s.cancelRun == nil {
		return
	}
	gen := s.gen
	s.idleTimer = time.AfterFunc(s.idle, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.viewers == 0 && s.gen == gen && s.cancelRun != nil {
			log.Printf("stream %s: idle, stopping source", s.id)
			s.stop()
		}
	})
}

// remove stops the source and disconnects every viewer.
func (s *Stream) remove() {
	s.mu.Lock()
	s.removed = true
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	if s.cancelRun != nil {
		s.stop()
	}
	s.mu.Unlock()
	s.hub.Close()
}

// start launches a run. Caller holds s.mu.
func (s *Stream) start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.gen++
	s.cancelRun = cancel
	gen := s.gen
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx, gen)
	}()
}

// stop cancels the current run and marks the stream idle. Caller holds s.mu.
func (s *Stream) stop() {
	s.cancelRun()
	s.cancelRun = nil
	s.gen++ // fences off updates from the run that is shutting down
	s.codec = ""
	s.hub.SetInit()
	s.setStateLocked(StateIdle, "")
}

func (s *Stream) run(ctx context.Context, gen int) {
	s.setState(gen, StateConnecting, "")
	backoff := minBackoff
	for {
		// On retries the failure state and message stay visible until video
		// flows again.
		gotVideo := false
		err := s.runner(ctx, s.url, func(seg mp4.Segment) {
			if !s.current(gen) {
				return
			}
			if seg.Init {
				s.onInit(gen, seg)
				return
			}
			if !gotVideo {
				gotVideo = true
				s.setState(gen, StateLive, "")
			}
			s.hub.Publish(hub.Message{Data: seg.Data})
		})
		if ctx.Err() != nil {
			return
		}

		msg, permanent := Classify(err)
		if gotVideo && !permanent && (msg == msgFailed || msg == msgEnded) {
			msg = "Connection to camera lost"
		}
		log.Printf("stream %s: %s: %s", s.id, msg, s.redact(err.Error()))
		if gotVideo {
			backoff = minBackoff
		}
		state := StateReconnecting
		if permanent {
			state = StateError
		}
		s.clearInit(gen)
		s.setState(gen, state, msg)

		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (s *Stream) onInit(gen int, seg mp4.Segment) {
	codecMsg, _ := json.Marshal(map[string]string{"type": "init", "codec": seg.Codec})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return
	}
	s.codec = seg.Codec
	s.hub.SetInit(hub.Message{Text: true, Data: codecMsg}, hub.Message{Data: seg.Data})
}

func (s *Stream) clearInit(gen int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == gen {
		s.codec = ""
		s.hub.SetInit()
	}
}

func (s *Stream) current(gen int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen == gen
}

func (s *Stream) setState(gen int, st State, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen == gen {
		s.setStateLocked(st, msg)
	}
}

func (s *Stream) setStateLocked(st State, msg string) {
	if s.state == st && s.errMsg == msg {
		return
	}
	s.state, s.errMsg = st, msg
	b, _ := json.Marshal(map[string]string{"type": "status", "state": string(st), "error": msg})
	s.hub.SetStatus(hub.Message{Text: true, Data: b})
}

// redact hides credentials that FFmpeg may echo back in its log.
func (s *Stream) redact(text string) string {
	if s.url == s.safeURL {
		return text
	}
	return strings.ReplaceAll(text, s.url, s.safeURL)
}
