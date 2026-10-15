package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/mp4"
)

// Runner pulls one RTSP source until it fails or ctx is cancelled, passing
// each fMP4 segment to onSegment. It always returns a non-nil error.
type Runner func(ctx context.Context, url string, onSegment func(mp4.Segment)) error

// RunError is a source failure with the tail of FFmpeg's log.
type RunError struct {
	Err    error
	Stderr []string
}

func (e *RunError) Error() string {
	if len(e.Stderr) > 0 {
		return fmt.Sprintf("%v: %s", e.Err, e.Stderr[len(e.Stderr)-1])
	}
	return e.Err.Error()
}

func (e *RunError) Unwrap() error { return e.Err }

var errStreamEnded = errors.New("stream ended")

// FFmpegArgs repackages the source's H.264 video into fragmented MP4 on
// stdout without re-encoding. Each fragment starts on a keyframe.
func FFmpegArgs(url string) []string {
	return []string{
		"-hide_banner", "-loglevel", "warning", "-nostdin",
		"-rtsp_transport", "tcp",
		"-timeout", "5000000", // socket I/O timeout, µs: a dead camera fails fast
		"-i", url,
		"-map", "0:v:0",
		"-c:v", "copy", "-an",
		"-f", "mp4", "-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"pipe:1",
	}
}

// FFmpegRunner runs the ffmpeg binary at path.
func FFmpegRunner(path string) Runner {
	return func(parent context.Context, url string, onSegment func(mp4.Segment)) error {
		ctx, cancel := context.WithCancel(parent)
		defer cancel()

		cmd := exec.CommandContext(ctx, path, FFmpegArgs(url)...)
		cmd.WaitDelay = 2 * time.Second
		stderr := &tailWriter{max: 20}
		cmd.Stderr = stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return &RunError{Err: fmt.Errorf("start ffmpeg: %w", err)}
		}

		seg := mp4.NewSegmenter(stdout)
		var readErr error
		for {
			s, err := seg.Next()
			if err != nil {
				readErr = err
				break
			}
			onSegment(s)
		}
		// FFmpeg dying mid-write truncates the last box; that's a source
		// failure, not bad output.
		badOutput := readErr != io.EOF && readErr != io.ErrUnexpectedEOF
		if badOutput {
			// FFmpeg may still be running: stop it.
			cancel()
		}
		_, _ = io.Copy(io.Discard, stdout)
		waitErr := cmd.Wait()

		if parent.Err() != nil {
			return parent.Err()
		}
		switch {
		case badOutput:
			err = fmt.Errorf("parse output: %w", readErr)
		case waitErr != nil:
			err = waitErr
		default:
			err = errStreamEnded
		}
		return &RunError{Err: err, Stderr: stderr.Lines()}
	}
}

// Classify turns a run failure into a message for users and reports whether
// retrying is unlikely to help without someone fixing the URL or camera.
func Classify(err error) (msg string, permanent bool) {
	var lines []string
	var re *RunError
	if errors.As(err, &re) {
		lines = re.Stderr
		if errors.Is(re.Err, exec.ErrNotFound) {
			return "FFmpeg is not installed on the server", true
		}
	}
	log := strings.ToLower(strings.Join(lines, "\n"))
	switch {
	case strings.Contains(log, "401 unauthorized"):
		return "Bad credentials", true
	case strings.Contains(log, "403 forbidden"):
		return "Access to stream denied", true
	case strings.Contains(log, "404 not found"):
		return "Stream path not found", true
	case strings.Contains(log, "400 bad request"):
		// MediaMTX answers 400 for unknown paths.
		return "Camera rejected the request (check the stream path)", true
	case strings.Contains(log, "connection refused"):
		return "Camera unreachable (connection refused)", false
	// -138 is ETIMEDOUT in FFmpeg's Windows builds. Don't match a bare
	// "timeout": FFmpeg echoes "?timeout=" in connection URLs.
	case strings.Contains(log, "timed out"), strings.Contains(log, "error number -138"):
		return "Camera did not respond (timed out)", false
	case strings.Contains(log, "failed to resolve"), strings.Contains(log, "name or service not known"),
		strings.Contains(log, "no such host"), strings.Contains(log, "nodename nor servname"):
		return "Camera host not found", true
	case strings.Contains(log, "no route to host"), strings.Contains(log, "network is unreachable"):
		return "Camera network unreachable", false
	case strings.Contains(log, "does not contain any stream"), strings.Contains(log, "matches no streams"):
		return "Stream has no video track", true
	case strings.Contains(log, "invalid data found"):
		return "Camera sent invalid data", false
	}
	if errors.Is(err, errStreamEnded) {
		return msgEnded, false
	}
	return msgFailed, false
}

const (
	msgEnded  = "Stream ended"
	msgFailed = "Stream failed"
)

// tailWriter keeps the last max lines written to it.
type tailWriter struct {
	mu      sync.Mutex
	max     int
	lines   []string
	partial []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.partial = append(t.partial, p...)
	for {
		i := bytes.IndexAny(t.partial, "\r\n")
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(t.partial[:i])); line != "" {
			t.lines = append(t.lines, line)
			if len(t.lines) > t.max {
				t.lines = t.lines[len(t.lines)-t.max:]
			}
		}
		t.partial = t.partial[i+1:]
	}
	if len(t.partial) > 4096 { // a "line" this long is noise
		t.partial = t.partial[:0]
	}
	return len(p), nil
}

func (t *tailWriter) Lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]string(nil), t.lines...)
	if line := strings.TrimSpace(string(t.partial)); line != "" {
		out = append(out, line)
	}
	return out
}
