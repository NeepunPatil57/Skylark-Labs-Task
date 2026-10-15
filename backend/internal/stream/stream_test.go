package stream

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/hub"
	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/mp4"
)

// fakeSource stands in for FFmpeg. behave decides, per call, which codec the
// source reports or whether it fails before sending anything.
type fakeSource struct {
	behave func(call int, opts RunOptions) (codec string, err error)

	mu     sync.Mutex
	calls  []RunOptions
	active int
}

func liveRunner(behave func(int, RunOptions) (string, error)) Runner {
	return (&fakeSource{behave: behave}).run
}

func (f *fakeSource) run(ctx context.Context, _ string, opts RunOptions, onSegment func(mp4.Segment) error) error {
	f.mu.Lock()
	call := len(f.calls)
	f.calls = append(f.calls, opts)
	f.active++
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	codec := "avc1.4D401F"
	if f.behave != nil {
		var err error
		if codec, err = f.behave(call, opts); err != nil {
			return err
		}
	}
	if err := onSegment(mp4.Segment{Init: true, Codec: codec, Data: []byte("init")}); err != nil {
		return err
	}
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if err := onSegment(mp4.Segment{Data: []byte("seg")}); err != nil {
				return err
			}
		}
	}
}

func (f *fakeSource) stats() (calls []RunOptions, active int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RunOptions(nil), f.calls...), f.active
}

type wireMsg struct {
	Type        string `json:"type"`
	State       State  `json:"state"`
	Error       string `json:"error"`
	Codec       string `json:"codec"`
	Transcoding bool   `json:"transcoding"`
}

// waitFor reads messages until one matches, failing after a timeout. It
// returns the decoded text messages seen, including the match.
func waitFor(t *testing.T, c *hub.Client, match func(wireMsg) bool) []wireMsg {
	t.Helper()
	var seen []wireMsg
	timeout := time.After(3 * time.Second)
	for {
		select {
		case m, ok := <-c.C:
			if !ok {
				t.Fatalf("client closed; saw %+v", seen)
			}
			if !m.Text {
				continue
			}
			var w wireMsg
			if err := json.Unmarshal(m.Data, &w); err != nil {
				t.Fatalf("bad JSON %q: %v", m.Data, err)
			}
			seen = append(seen, w)
			if match(w) {
				return seen
			}
		case <-timeout:
			t.Fatalf("timed out; saw %+v", seen)
		}
	}
}

func state(s State) func(wireMsg) bool {
	return func(w wireMsg) bool { return w.Type == "status" && w.State == s }
}

func waitClosed(t *testing.T, c *hub.Client) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-c.C:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("client was not closed")
		}
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func addStream(t *testing.T, f *fakeSource) (*Manager, *Stream) {
	t.Helper()
	m := NewManager(Options{Runner: f.run, MaxStreams: 4, IdleTimeout: 50 * time.Millisecond})
	t.Cleanup(m.Close)
	info, _, err := m.Add("rtsp://cam/test", "")
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m.Get(info.ID)
	return m, s
}

func TestStartsOnFirstViewerAndGoesLive(t *testing.T) {
	f := &fakeSource{}
	_, s := addStream(t, f)

	time.Sleep(20 * time.Millisecond)
	if calls, _ := f.stats(); len(calls) != 0 {
		t.Fatal("source started with no viewers")
	}

	c, _ := s.Subscribe()
	seen := waitFor(t, c, state(StateLive))
	if seen[0].State != StateConnecting {
		t.Errorf("first message = %+v, want connecting status", seen[0])
	}
	var init *wireMsg
	for i := range seen {
		if seen[i].Type == "init" {
			init = &seen[i]
		}
	}
	if init == nil || init.Codec != "avc1.4D401F" || init.Transcoding {
		t.Errorf("init message = %+v", init)
	}
	if info := s.Info(); info.State != StateLive || info.Viewers != 1 || info.Codec != "avc1.4D401F" {
		t.Errorf("Info = %+v", info)
	}
}

func TestSecondViewerSharesSourceAndJoinsMidStream(t *testing.T) {
	f := &fakeSource{}
	_, s := addStream(t, f)
	a, _ := s.Subscribe()
	waitFor(t, a, state(StateLive))

	b, _ := s.Subscribe()
	seen := waitFor(t, b, func(w wireMsg) bool { return w.Type == "init" })
	if seen[0].State != StateLive {
		t.Errorf("late joiner first got %+v, want live status replay", seen[0])
	}
	if calls, _ := f.stats(); len(calls) != 1 {
		t.Errorf("source started %d times, want 1 shared run", len(calls))
	}
}

func TestStopsAfterIdleTimeoutAndRestarts(t *testing.T) {
	f := &fakeSource{}
	_, s := addStream(t, f)
	c, _ := s.Subscribe()
	waitFor(t, c, state(StateLive))

	s.Unsubscribe(c)
	eventually(t, "source to stop", func() bool { _, active := f.stats(); return active == 0 })
	if st := s.Info().State; st != StateIdle {
		t.Errorf("state = %s, want idle", st)
	}

	c2, _ := s.Subscribe()
	waitFor(t, c2, state(StateLive))
	if calls, _ := f.stats(); len(calls) != 2 {
		t.Errorf("source started %d times, want 2", len(calls))
	}
}

func TestQuickResubscribeKeepsSourceRunning(t *testing.T) {
	f := &fakeSource{}
	_, s := addStream(t, f)
	c, _ := s.Subscribe()
	waitFor(t, c, state(StateLive))

	s.Unsubscribe(c)
	c2, _ := s.Subscribe() // before the idle timeout fires
	time.Sleep(100 * time.Millisecond)
	if calls, active := f.stats(); len(calls) != 1 || active != 1 {
		t.Errorf("calls=%d active=%d, want the original run to continue", len(calls), active)
	}
	s.Unsubscribe(c2)
}

func TestSwitchesToTranscodingForUnplayableCodec(t *testing.T) {
	f := &fakeSource{behave: func(_ int, opts RunOptions) (string, error) {
		if opts.Transcode {
			return "avc1.42C01F", nil
		}
		return "hvc1", nil
	}}
	_, s := addStream(t, f)
	c, _ := s.Subscribe()

	seen := waitFor(t, c, state(StateLive))
	for _, w := range seen {
		if w.Type == "init" && w.Codec == "hvc1" {
			t.Error("unplayable init segment was sent to viewers")
		}
		if w.State == StateReconnecting || w.State == StateError {
			t.Errorf("switching to transcoding showed %s", w.State)
		}
	}
	if last := seen[len(seen)-2]; last.Type != "init" || !last.Transcoding || last.Codec != "avc1.42C01F" {
		t.Errorf("init before live = %+v, want transcoded H.264", last)
	}
	calls, _ := f.stats()
	if len(calls) != 2 || calls[0].Transcode || !calls[1].Transcode {
		t.Errorf("runs = %+v, want copy then transcode", calls)
	}
	if !s.Info().Transcoding {
		t.Error("Info().Transcoding = false")
	}
}

func TestReconnectsWithFriendlyError(t *testing.T) {
	refused := &RunError{Err: errors.New("exit status 1"), Stderr: []string{"Connection to tcp://cam:554 failed: Connection refused"}}
	f := &fakeSource{behave: func(call int, _ RunOptions) (string, error) {
		if call == 0 {
			return "", refused
		}
		return "avc1.4D401F", nil
	}}
	_, s := addStream(t, f)
	c, _ := s.Subscribe()

	seen := waitFor(t, c, state(StateReconnecting))
	if got := seen[len(seen)-1].Error; got != "Camera unreachable (connection refused)" {
		t.Errorf("error = %q", got)
	}
	start := time.Now()
	waitFor(t, c, state(StateLive))
	if waited := time.Since(start); waited < minBackoff/2 {
		t.Errorf("retried after %v, want a backoff of about %v", waited, minBackoff)
	}
}

func TestPermanentErrorAndViewerKick(t *testing.T) {
	unauthorized := &RunError{Err: errors.New("exit status 1"), Stderr: []string{"method DESCRIBE failed: 401 Unauthorized"}}
	f := &fakeSource{behave: func(int, RunOptions) (string, error) { return "", unauthorized }}
	_, s := addStream(t, f)
	c, _ := s.Subscribe()

	seen := waitFor(t, c, state(StateError))
	if got := seen[len(seen)-1].Error; got != "Bad credentials" {
		t.Errorf("error = %q, want Bad credentials", got)
	}

	// A new viewer (e.g. the Retry button) skips the backoff wait.
	c2, _ := s.Subscribe()
	eventually(t, "immediate retry", func() bool { calls, _ := f.stats(); return len(calls) >= 2 })
	s.Unsubscribe(c2)
}
