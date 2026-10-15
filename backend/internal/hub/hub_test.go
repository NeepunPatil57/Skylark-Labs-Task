package hub

import (
	"testing"
)

func text(s string) Message { return Message{Text: true, Data: []byte(s)} }
func bin(s string) Message  { return Message{Data: []byte(s)} }

// drain returns everything queued for c without blocking.
func drain(c *Client) []string {
	var out []string
	for {
		select {
		case m, ok := <-c.C:
			if !ok {
				return append(out, "<closed>")
			}
			out = append(out, string(m.Data))
		default:
			return out
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLateJoinerGetsStatusAndInit(t *testing.T) {
	h := New(4)
	h.SetStatus(text("connecting"))
	h.SetStatus(text("live"))
	h.SetInit(text("codec"), bin("init"))
	h.Publish(bin("seg1")) // before the viewer joins: not replayed

	c, ok := h.Subscribe()
	if !ok {
		t.Fatal("Subscribe failed")
	}
	h.Publish(bin("seg2"))

	want := []string{"live", "codec", "init", "seg2"}
	if got := drain(c); !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFanOut(t *testing.T) {
	h := New(4)
	a, _ := h.Subscribe()
	b, _ := h.Subscribe()
	h.Publish(bin("seg"))
	for name, c := range map[string]*Client{"a": a, "b": b} {
		if got := drain(c); !equal(got, []string{"seg"}) {
			t.Errorf("client %s got %v", name, got)
		}
	}
	if h.Len() != 2 {
		t.Errorf("Len = %d, want 2", h.Len())
	}
}

// A client's queue holds bufSize messages plus one slot reserved for the
// status replayed on Subscribe.

func TestSlowClientSkipsMediaWithoutBlocking(t *testing.T) {
	h := New(2) // queue of 3
	slow, _ := h.Subscribe()
	fast, _ := h.Subscribe()

	for _, s := range []string{"s1", "s2", "s3", "s4"} {
		h.Publish(bin(s)) // must not block on the full slow client
		drain(fast)
	}

	if got := drain(slow); !equal(got, []string{"s1", "s2", "s3"}) {
		t.Errorf("slow client got %v, want only what fit in its queue", got)
	}
	h.Publish(bin("s5"))
	if got := drain(slow); !equal(got, []string{"s5"}) {
		t.Errorf("slow client after catching up got %v, want [s5]", got)
	}
}

func TestSlowClientDroppedOnControlMessage(t *testing.T) {
	h := New(1) // queue of 2
	slow, _ := h.Subscribe()
	h.Publish(bin("s1"))
	h.Publish(bin("s2")) // queue full
	h.SetStatus(text("reconnecting"))

	if got := drain(slow); !equal(got, []string{"s1", "s2", "<closed>"}) {
		t.Errorf("got %v: a client that can't take a state change must be closed", got)
	}
	if h.Len() != 0 {
		t.Errorf("Len = %d, want 0", h.Len())
	}
}

func TestSetInitEmptyClearsReplay(t *testing.T) {
	h := New(4)
	h.SetInit(text("codec"), bin("init"))
	h.SetInit()
	c, _ := h.Subscribe()
	if got := drain(c); len(got) != 0 {
		t.Errorf("got %v, want nothing replayed", got)
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	h := New(4)
	c, _ := h.Subscribe()
	h.Unsubscribe(c)
	h.Unsubscribe(c) // must not panic on double close
	if got := drain(c); !equal(got, []string{"<closed>"}) {
		t.Errorf("got %v", got)
	}
}

func TestCloseRejectsNewClients(t *testing.T) {
	h := New(4)
	c, _ := h.Subscribe()
	h.Close()
	if got := drain(c); !equal(got, []string{"<closed>"}) {
		t.Errorf("existing client got %v, want closed", got)
	}
	if _, ok := h.Subscribe(); ok {
		t.Error("Subscribe after Close succeeded")
	}
	h.Unsubscribe(c) // still safe
}
