package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/stream"
)

func newServer(t *testing.T, origins string) *httptest.Server {
	t.Helper()
	m := stream.NewManager(stream.Options{MaxStreams: 2, IdleTimeout: time.Second})
	t.Cleanup(m.Close)
	mux := http.NewServeMux()
	Register(mux, m)
	srv := httptest.NewServer(CORS(ParseOrigins(origins), mux))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

func TestStreamsCRUD(t *testing.T) {
	srv := newServer(t, "")
	base := srv.URL + "/api/streams"

	res, body := do(t, "POST", base, `{"url":"rtsp://admin:pw@cam/live?token=abc","name":"Gate"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, body %v", res.StatusCode, body)
	}
	id, _ := body["id"].(string)
	if body["url"] != "rtsp://admin:xxxxx@cam/live?xxxxx" || body["name"] != "Gate" || id == "" {
		t.Errorf("POST body = %v", body)
	}

	if res, body := do(t, "POST", base, `{"url":"rtsp://admin:pw@cam/live?token=abc"}`); res.StatusCode != http.StatusOK || body["id"] != id {
		t.Errorf("duplicate POST = %d %v, want 200 with the same id", res.StatusCode, body)
	}

	listRes, err := http.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	_ = json.NewDecoder(listRes.Body).Decode(&list)
	listRes.Body.Close()
	if len(list) != 1 || list[0]["id"] != id {
		t.Errorf("GET list = %v", list)
	}

	if res, _ := do(t, "GET", base+"/"+id, ""); res.StatusCode != http.StatusOK {
		t.Errorf("GET one = %d", res.StatusCode)
	}
	if res, _ := do(t, "DELETE", base+"/"+id, ""); res.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE = %d, want 204", res.StatusCode)
	}
	if res, _ := do(t, "DELETE", base+"/"+id, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("second DELETE = %d, want 404", res.StatusCode)
	}
	if res, _ := do(t, "GET", base+"/"+id, ""); res.StatusCode != http.StatusNotFound {
		t.Errorf("GET removed = %d, want 404", res.StatusCode)
	}
}

func TestAddStreamErrors(t *testing.T) {
	srv := newServer(t, "")
	base := srv.URL + "/api/streams"
	tests := []struct {
		body   string
		status int
		errHas string
	}{
		{`not json`, http.StatusBadRequest, "JSON"},
		{`{"url":"http://cam/x"}`, http.StatusUnprocessableEntity, "rtsp://"},
		{`{"url":""}`, http.StatusUnprocessableEntity, "required"},
	}
	for _, tt := range tests {
		res, body := do(t, "POST", base, tt.body)
		msg, _ := body["error"].(string)
		if res.StatusCode != tt.status || !strings.Contains(msg, tt.errHas) {
			t.Errorf("POST %s = %d %q, want %d containing %q", tt.body, res.StatusCode, msg, tt.status, tt.errHas)
		}
	}

	do(t, "POST", base, `{"url":"rtsp://cam/1"}`)
	do(t, "POST", base, `{"url":"rtsp://cam/2"}`)
	if res, body := do(t, "POST", base, `{"url":"rtsp://cam/3"}`); res.StatusCode != http.StatusConflict {
		t.Errorf("over limit = %d %v, want 409", res.StatusCode, body)
	}
}

func TestCORS(t *testing.T) {
	srv := newServer(t, "https://app.example.com, http://localhost:5173/")

	req, _ := http.NewRequest("OPTIONS", srv.URL+"/api/streams", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Errorf("preflight = %d, allow-origin %q", res.StatusCode, res.Header.Get("Access-Control-Allow-Origin"))
	}

	req, _ = http.NewRequest("GET", srv.URL+"/api/streams", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unlisted origin got Access-Control-Allow-Origin %q", got)
	}
}

func TestParseOrigins(t *testing.T) {
	o := ParseOrigins(" https://A.example.com/ ,http://localhost:5173")
	if !o.Allowed("https://a.example.com") || !o.Allowed("http://localhost:5173") || o.Allowed("https://b.example.com") || o.Any() {
		t.Errorf("unexpected origin matching: %+v", o)
	}
	hosts := strings.Join(o.Hosts(), ",")
	if !strings.Contains(hosts, "a.example.com") || !strings.Contains(hosts, "localhost:5173") {
		t.Errorf("Hosts() = %s", hosts)
	}
	if all := ParseOrigins("*"); !all.Any() || !all.Allowed("https://whatever.dev") {
		t.Error(`"*" must allow any origin`)
	}
}
