package stream

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

// fakeResolver maps host names to addresses; unknown names fail to resolve.
type fakeResolver map[string][]string

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	ips, ok := f[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	out := make([]netip.Addr, len(ips))
	for i, ip := range ips {
		out[i] = netip.MustParseAddr(ip)
	}
	return out, nil
}

func TestURLGuard(t *testing.T) {
	g := NewURLGuard([]string{"localhost:8554", " Cams.Internal "}, fakeResolver{
		"cam.example.com":  {"93.184.216.34"},
		"v6.example.com":   {"2606:4700::1111"},
		"internal.example": {"10.1.2.3"},
		"mixed.example":    {"93.184.216.34", "192.168.0.10"},
		"metadata.example": {"169.254.169.254"},
		"localhost":        {"127.0.0.1", "::1"},
	})
	tests := []struct {
		in      string
		allowed bool
	}{
		{"rtsp://cam.example.com/live", true},
		{"rtsp://v6.example.com/live", true},
		{"rtsp://8.8.8.8:554/live", true},
		{"rtsp://localhost:8554/cam1", true}, // allow list
		{"rtsp://LOCALHOST:8554/cam1", true}, // case-insensitive
		{"rtsp://localhost/cam1", false},     // allow list is host:port
		{"rtsp://localhost:6379/", false},
		{"rtsp://127.0.0.1:8554/cam1", false}, // allow list matches names, not IPs
		{"rtsp://internal.example/s", false},
		{"rtsp://mixed.example/s", false}, // any internal address blocks
		{"rtsp://metadata.example/s", false},
		{"rtsp://169.254.169.254/latest", false},
		{"rtsp://10.0.0.5/s", false},
		{"rtsp://172.16.0.1/s", false},
		{"rtsp://192.168.1.20:554/stream1", false},
		{"rtsp://100.64.0.1/s", false},
		{"rtsp://0.0.0.0/s", false},
		{"rtsp://[::1]/s", false},
		{"rtsp://[fd12::1]/s", false},
		{"rtsp://[fe80::1]/s", false},
		{"rtsp://[::ffff:10.0.0.1]/s", false}, // IPv4-mapped
		{"rtsp://[64:ff9b::a00:1]/s", false},  // NAT64 of 10.0.0.1
		{"rtsp://nowhere.invalid/s", false},   // unresolvable
	}
	for _, tt := range tests {
		u, err := ValidateURL(tt.in)
		if err != nil {
			t.Fatalf("ValidateURL(%q): %v", tt.in, err)
		}
		err = g.Check(u)
		if tt.allowed && err != nil {
			t.Errorf("Check(%q) = %v, want allowed", tt.in, err)
		}
		if !tt.allowed && !errors.Is(err, ErrInvalidURL) {
			t.Errorf("Check(%q) = %v, want ErrInvalidURL", tt.in, err)
		}
	}
}

func TestURLGuardDefaultPorts(t *testing.T) {
	g := NewURLGuard([]string{"cams.internal:554", "secure.internal:322"}, fakeResolver{})
	for _, in := range []string{"rtsp://cams.internal/a", "rtsps://secure.internal/a"} {
		u, _ := ValidateURL(in)
		if err := g.Check(u); err != nil {
			t.Errorf("Check(%q) = %v, want allowed via default port", in, err)
		}
	}
}

func TestManagerAddRunsCheckURL(t *testing.T) {
	g := NewURLGuard(nil, fakeResolver{})
	m := NewManager(Options{MaxStreams: 4, CheckURL: g.Check})
	if _, _, err := m.Add("rtsp://10.0.0.5/s", ""); !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("Add internal URL err = %v, want ErrInvalidURL", err)
	}
	if got := len(m.List()); got != 0 {
		t.Fatalf("blocked URL was registered: %d streams", got)
	}
	if _, _, err := m.Add("rtsp://8.8.8.8/s", ""); err != nil {
		t.Fatalf("Add public URL: %v", err)
	}
}
