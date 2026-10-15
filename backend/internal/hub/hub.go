// Package hub fans one producer's messages out to many subscribers without
// letting a slow subscriber stall the others.
package hub

import "sync"

// Message is one WebSocket frame: JSON text or binary media.
type Message struct {
	Text bool
	Data []byte
}

// Client is one subscriber. C is closed when the client is removed, either by
// Unsubscribe, by the hub closing, or because it fell too far behind on
// control messages.
type Client struct {
	C  <-chan Message
	ch chan Message
}

type Hub struct {
	mu      sync.Mutex
	clients map[*Client]struct{}
	bufSize int
	closed  bool

	// Replayed to late joiners so they can start mid-stream.
	status   *Message
	initMsgs []Message
}

func New(bufSize int) *Hub {
	return &Hub{clients: make(map[*Client]struct{}), bufSize: bufSize}
}

// Subscribe adds a client. Its queue starts with the latest status and, if
// the stream is live, the codec message and init segment, so every later
// media segment is playable. Returns false if the hub is closed.
func (h *Hub) Subscribe() (*Client, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, false
	}
	ch := make(chan Message, h.bufSize+len(h.initMsgs)+1)
	if h.status != nil {
		ch <- *h.status
	}
	for _, m := range h.initMsgs {
		ch <- m
	}
	c := &Client{C: ch, ch: ch}
	h.clients[c] = struct{}{}
	return c, true
}

// Unsubscribe removes a client. Safe to call more than once.
func (h *Hub) Unsubscribe(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remove(c)
}

// SetStatus records and broadcasts a status message. Clients that can't take
// it are dropped, since missing a state change would leave their UI wrong.
func (h *Hub) SetStatus(m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = &m
	h.broadcastOrDrop(m)
}

// SetInit records and broadcasts the messages a player needs before media
// (codec info, then init segment). An empty call clears them, e.g. while the
// source is reconnecting.
func (h *Hub) SetInit(msgs ...Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.initMsgs = msgs
	for _, m := range msgs {
		h.broadcastOrDrop(m)
	}
}

// Publish broadcasts a media segment. A client whose queue is full skips
// this segment; every segment starts on a keyframe, so playback resumes
// cleanly with the next one.
func (h *Hub) Publish(m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.ch <- m:
		default:
		}
	}
}

// Len returns the number of subscribers.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Close removes every client and rejects new ones.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for c := range h.clients {
		h.remove(c)
	}
}

func (h *Hub) broadcastOrDrop(m Message) {
	for c := range h.clients {
		select {
		case c.ch <- m:
		default:
			h.remove(c)
		}
	}
}

func (h *Hub) remove(c *Client) {
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.ch)
	}
}
