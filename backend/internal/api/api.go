// Package api serves the REST endpoints for managing streams.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/stream"
)

type addRequest struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Register mounts the stream endpoints on mux.
func Register(mux *http.ServeMux, m *stream.Manager) {
	mux.HandleFunc("GET /api/streams", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, m.List())
	})

	mux.HandleFunc("POST /api/streams", func(w http.ResponseWriter, r *http.Request) {
		var req addRequest
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "request body must be JSON: {\"url\": \"rtsp://...\", \"name\": \"...\"}")
			return
		}
		info, created, err := m.Add(req.URL, req.Name)
		switch {
		case errors.Is(err, stream.ErrInvalidURL):
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, stream.ErrLimit):
			writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			log.Printf("add stream: %v", err)
			writeError(w, http.StatusInternalServerError, "could not add stream")
		case created:
			log.Printf("stream %s: added %s", info.ID, info.URL)
			writeJSON(w, http.StatusCreated, info)
		default:
			writeJSON(w, http.StatusOK, info)
		}
	})

	mux.HandleFunc("GET /api/streams/{id}", func(w http.ResponseWriter, r *http.Request) {
		s, ok := m.Get(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, stream.ErrNotFound.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.Info())
	})

	mux.HandleFunc("DELETE /api/streams/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := m.Remove(id); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		log.Printf("stream %s: removed", id)
		w.WriteHeader(http.StatusNoContent)
	})
}

// Origins is the set of browser origins allowed to call the API.
type Origins struct {
	any   bool
	exact map[string]bool
}

// ParseOrigins reads a comma-separated list such as
// "https://app.vercel.app,http://localhost:5173". "*" allows any origin.
func ParseOrigins(list string) Origins {
	o := Origins{exact: make(map[string]bool)}
	for _, v := range strings.Split(list, ",") {
		v = strings.TrimRight(strings.TrimSpace(v), "/")
		switch v {
		case "":
		case "*":
			o.any = true
		default:
			o.exact[strings.ToLower(v)] = true
		}
	}
	return o
}

func (o Origins) Any() bool { return o.any }

func (o Origins) Allowed(origin string) bool {
	return o.any || o.exact[strings.ToLower(origin)]
}

// Hosts returns host[:port] patterns for the WebSocket origin check.
func (o Origins) Hosts() []string {
	var hosts []string
	for v := range o.exact {
		if u, err := url.Parse(v); err == nil && u.Host != "" {
			hosts = append(hosts, u.Host)
		}
	}
	return hosts
}

// CORS lets the allowed origins call the API from the browser.
func CORS(o Origins, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && o.Allowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
