// Package stream manages RTSP sources: one FFmpeg process per unique URL,
// started on demand, restarted with backoff, shared by all viewers.
package stream

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/hub"
)

var (
	ErrInvalidURL = errors.New("invalid stream URL")
	ErrLimit      = errors.New("stream limit reached")
	ErrNotFound   = errors.New("stream not found")
)

type Options struct {
	Runner      Runner
	MaxStreams  int
	IdleTimeout time.Duration // how long a source keeps running with no viewers
}

type Manager struct {
	opts Options
	wg   sync.WaitGroup

	mu      sync.Mutex
	streams map[string]*Stream
	order   []string // insertion order, for stable listing
}

func NewManager(opts Options) *Manager {
	return &Manager{opts: opts, streams: make(map[string]*Stream)}
}

// Add registers a stream. Adding a URL that is already registered returns
// the existing stream with created=false.
func (m *Manager) Add(rawURL, name string) (info Info, created bool, err error) {
	u, err := ValidateURL(rawURL)
	if err != nil {
		return Info{}, false, err
	}
	id := streamID(u)

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.streams[id]; ok {
		return s.Info(), false, nil
	}
	if len(m.streams) >= m.opts.MaxStreams {
		return Info{}, false, fmt.Errorf("%w (max %d)", ErrLimit, m.opts.MaxStreams)
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.TrimPrefix(u.Host+u.Path, "/")
	}
	if len(name) > 64 {
		name = name[:64]
	}
	s := &Stream{
		id:      id,
		name:    name,
		url:     u.String(),
		safeURL: u.Redacted(),
		runner:  m.opts.Runner,
		idle:    m.opts.IdleTimeout,
		wg:      &m.wg,
		hub:     hub.New(clientBufSize),
		kick:    make(chan struct{}),
		state:   StateIdle,
	}
	m.streams[id] = s
	m.order = append(m.order, id)
	return s.Info(), true, nil
}

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.streams[id].Info())
	}
	return out
}

func (m *Manager) Get(id string) (*Stream, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.streams[id]
	return s, ok
}

// Remove stops a stream and disconnects its viewers.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	s, ok := m.streams[id]
	if ok {
		delete(m.streams, id)
		for i, v := range m.order {
			if v == id {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
	}
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	s.remove()
	return nil
}

// Close stops every stream and waits for their FFmpeg processes to exit.
func (m *Manager) Close() {
	m.mu.Lock()
	streams := make([]*Stream, 0, len(m.streams))
	for _, s := range m.streams {
		streams = append(streams, s)
	}
	m.mu.Unlock()
	for _, s := range streams {
		s.remove()
	}
	m.wg.Wait()
}

// ValidateURL accepts rtsp:// and rtsps:// URLs with a host.
func ValidateURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: URL is required", ErrInvalidURL)
	}
	if len(raw) > 2048 {
		return nil, fmt.Errorf("%w: URL is too long", ErrInvalidURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: could not parse URL", ErrInvalidURL)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "rtsp" && u.Scheme != "rtsps" {
		return nil, fmt.Errorf("%w: must start with rtsp:// or rtsps://", ErrInvalidURL)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidURL)
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return nil, fmt.Errorf("%w: URL must not contain spaces", ErrInvalidURL)
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	return u, nil
}

func streamID(u *url.URL) string {
	sum := sha256.Sum256([]byte(u.String()))
	return hex.EncodeToString(sum[:6])
}
