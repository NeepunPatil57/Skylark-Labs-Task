package stream

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestFFmpegArgs(t *testing.T) {
	copyArgs := FFmpegArgs("rtsp://cam/s", RunOptions{})
	transcodeArgs := FFmpegArgs("rtsp://cam/s", RunOptions{Transcode: true})

	for name, args := range map[string][]string{"copy": copyArgs, "transcode": transcodeArgs} {
		joined := strings.Join(args, " ")
		for _, want := range []string{
			"-rtsp_transport tcp",
			"-i rtsp://cam/s",
			"-an",
			"-movflags frag_keyframe+empty_moov+default_base_moof",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s args missing %q: %s", name, want, joined)
			}
		}
		if args[len(args)-1] != "pipe:1" {
			t.Errorf("%s args must end with pipe:1", name)
		}
	}
	if !strings.Contains(strings.Join(copyArgs, " "), "-c:v copy") || slices.Contains(copyArgs, "libx264") {
		t.Error("copy mode must not re-encode")
	}
	if !strings.Contains(strings.Join(transcodeArgs, " "), "-c:v libx264") {
		t.Error("transcode mode must encode H.264")
	}
}

func TestBrowserPlayable(t *testing.T) {
	for codec, want := range map[string]bool{
		"avc1.64001F": true, "avc3.4D401E": true,
		"avc1": false, "hvc1": false, "hev1": false, "av01": false, "": false,
	} {
		if got := BrowserPlayable(codec); got != want {
			t.Errorf("BrowserPlayable(%q) = %v, want %v", codec, got, want)
		}
	}
}

func TestClassify(t *testing.T) {
	run := func(lines ...string) error { return &RunError{Err: errors.New("exit status 1"), Stderr: lines} }
	tests := []struct {
		err       error
		msg       string
		permanent bool
	}{
		{run("[rtsp] method DESCRIBE failed: 401 Unauthorized"), "Bad credentials", true},
		{run("Server returned 403 Forbidden (access denied)"), "Access to stream denied", true},
		{run("Server returned 404 Not Found"), "Stream path not found", true},
		{run("Server returned 400 Bad Request"), "Camera rejected the request (check the stream path)", true},
		{run("Connection to tcp://cam:554 failed: Connection refused"), "Camera unreachable (connection refused)", false},
		{run("Connection to tcp://cam:554?timeout=5000000 failed: Connection timed out"), "Camera did not respond (timed out)", false},
		{run("Connection to tcp://cam:554?timeout=5000000 failed: Error number -138 occurred"), "Camera did not respond (timed out)", false},
		{run("Failed to resolve hostname nope.invalid"), "Camera host not found", true},
		{run("Unknown encoder 'libx264'"), "Server cannot transcode this camera's video", true},
		{run("Invalid data found when processing input"), "Camera sent invalid data", false},
		// "?timeout=" in an echoed URL alone must not read as a timeout.
		{run("Opening tcp://cam:554?timeout=5000000", "something else broke"), msgFailed, false},
		{&RunError{Err: errStreamEnded}, msgEnded, false},
		{&RunError{Err: fmt.Errorf("start ffmpeg: %w", exec.ErrNotFound)}, "FFmpeg is not installed on the server", true},
		{context.DeadlineExceeded, msgFailed, false},
	}
	for _, tt := range tests {
		msg, permanent := Classify(tt.err)
		if msg != tt.msg || permanent != tt.permanent {
			t.Errorf("Classify(%v) = (%q, %v), want (%q, %v)", tt.err, msg, permanent, tt.msg, tt.permanent)
		}
	}
}

func TestTailWriter(t *testing.T) {
	w := &tailWriter{max: 2}
	fmt.Fprint(w, "one\r\ntwo\n\n")
	fmt.Fprint(w, "thr")
	fmt.Fprint(w, "ee\nfour (no newline)")
	got := w.Lines()
	want := []string{"two", "three", "four (no newline)"}
	if !slices.Equal(got, want) {
		t.Errorf("Lines() = %q, want %q", got, want)
	}
}
