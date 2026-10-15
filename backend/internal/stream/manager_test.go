package stream

import (
	"errors"
	"testing"
	"time"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		in      string
		want    string // normalized URL; empty means invalid
		comment string
	}{
		{"rtsp://cam.local:554/stream1", "rtsp://cam.local:554/stream1", ""},
		{"  RTSP://Cam.LOCAL/live  ", "rtsp://cam.local/live", "trims, lowercases scheme and host"},
		{"rtsps://user:pa%40ss@10.0.0.5/s", "rtsps://user:pa%40ss@10.0.0.5/s", "keeps encoded credentials"},
		{"rtsp://host/path#frag", "rtsp://host/path", "drops fragment"},
		{"", "", "empty"},
		{"http://host/stream", "", "wrong scheme"},
		{"host:554/stream", "", "no scheme"},
		{"rtsp:///path", "", "no host"},
		{"rtsp://host/a b", "", "space"},
		{"rtsp://" + string(make([]byte, 2100)), "", "too long"},
	}
	for _, tt := range tests {
		u, err := ValidateURL(tt.in)
		if tt.want == "" {
			if err == nil || !errors.Is(err, ErrInvalidURL) {
				t.Errorf("ValidateURL(%q) err = %v, want ErrInvalidURL (%s)", tt.in, err, tt.comment)
			}
			continue
		}
		if err != nil {
			t.Errorf("ValidateURL(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got := u.String(); got != tt.want {
			t.Errorf("ValidateURL(%q) = %q, want %q (%s)", tt.in, got, tt.want, tt.comment)
		}
	}
}

func TestMaskURL(t *testing.T) {
	tests := map[string]string{
		"rtsp://cam/stream":                         "rtsp://cam/stream",
		"rtsp://admin:secret@cam:554/stream":        "rtsp://admin:xxxxx@cam:554/stream",
		"rtsp://cam/live?token=abc123&x=1":          "rtsp://cam/live?xxxxx",
		"rtsp://admin:secret@cam/live?token=abc123": "rtsp://admin:xxxxx@cam/live?xxxxx",
		"rtsp://useronly@cam/live":                  "rtsp://useronly@cam/live",
	}
	for in, want := range tests {
		u, err := ValidateURL(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := MaskURL(u); got != want {
			t.Errorf("MaskURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func newTestManager(r Runner, max int) *Manager {
	return NewManager(Options{Runner: r, MaxStreams: max, IdleTimeout: 50 * time.Millisecond})
}

func TestManagerAddDedupesAndMasks(t *testing.T) {
	m := newTestManager(liveRunner(nil), 4)
	defer m.Close()

	a, created, err := m.Add("rtsp://u:p@cam/one", "Front door")
	if err != nil || !created {
		t.Fatalf("first Add: created=%v err=%v", created, err)
	}
	if a.URL != "rtsp://u:xxxxx@cam/one" || a.Name != "Front door" || a.State != StateIdle {
		t.Errorf("info = %+v", a)
	}

	b, created, err := m.Add(" rtsp://u:p@CAM/one ", "other name")
	if err != nil || created || b.ID != a.ID {
		t.Errorf("same URL again: id=%s created=%v err=%v, want existing %s", b.ID, created, err, a.ID)
	}

	c, _, _ := m.Add("rtsp://cam/two", "")
	if c.Name != "cam/two" {
		t.Errorf("default name = %q, want host+path", c.Name)
	}
	if got := len(m.List()); got != 2 {
		t.Errorf("List has %d streams, want 2", got)
	}
}

func TestManagerLimit(t *testing.T) {
	m := newTestManager(liveRunner(nil), 1)
	defer m.Close()
	if _, _, err := m.Add("rtsp://cam/1", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Add("rtsp://cam/2", ""); !errors.Is(err, ErrLimit) {
		t.Errorf("err = %v, want ErrLimit", err)
	}
	// Re-adding an existing URL is fine at the limit.
	if _, _, err := m.Add("rtsp://cam/1", ""); err != nil {
		t.Errorf("re-add at limit: %v", err)
	}
}

func TestManagerRemove(t *testing.T) {
	m := newTestManager(liveRunner(nil), 4)
	defer m.Close()
	info, _, _ := m.Add("rtsp://cam/1", "")
	s, _ := m.Get(info.ID)
	c, _ := s.Subscribe()

	if err := m.Remove(info.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(info.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Remove err = %v, want ErrNotFound", err)
	}
	waitClosed(t, c)
	if _, ok := s.Subscribe(); ok {
		t.Error("Subscribe on removed stream succeeded")
	}
}
