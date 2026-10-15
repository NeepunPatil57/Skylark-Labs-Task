package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/api"
	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/stream"
	"github.com/NeepunPatil57/Skylark-Labs-Task/backend/internal/ws"
)

func main() {
	addr := ":" + envOr("PORT", "8080")
	origins := api.ParseOrigins(envOr("ALLOWED_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173"))

	manager := stream.NewManager(stream.Options{
		Runner:      stream.FFmpegRunner(envOr("FFMPEG_PATH", "ffmpeg")),
		MaxStreams:  envInt("MAX_STREAMS", 16),
		IdleTimeout: time.Duration(envInt("IDLE_TIMEOUT_SECONDS", 30)) * time.Second,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	api.Register(mux, manager)
	mux.Handle("GET /ws/{id}", ws.Handler(manager, ws.Options{
		OriginHosts: origins.Hosts(),
		AnyOrigin:   origins.Any(),
	}))

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.CORS(origins, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")
	// Shutdown doesn't track WebSockets (hijacked connections), so close the
	// streams first: that stops FFmpeg and sends viewers a close frame.
	manager.Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		log.Printf("ignoring invalid %s=%q, using %d", key, v, fallback)
		return fallback
	}
	return n
}
