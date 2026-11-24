package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed web/*
var webFiles embed.FS

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	api := analytics{views: make(map[string]uint64)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("POST /api/events", api.addEvent)
	mux.HandleFunc("GET /api/stats", api.stats)
	mux.HandleFunc("GET /{$}", webFile("dashboard.html"))
	mux.HandleFunc("GET /demo/{$}", webFile("demo.html"))
	mux.HandleFunc("GET /demo/catalog", webFile("demo.html"))
	mux.HandleFunc("GET /demo/about", webFile("demo.html"))
	mux.HandleFunc("GET /styles.css", webFile("styles.css"))
	mux.HandleFunc("GET /dashboard.js", webFile("dashboard.js"))
	mux.HandleFunc("GET /tracker.js", webFile("tracker.js"))

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverDone := make(chan error, 1)
	go func() {
		log.Printf("LiveTraffic: http://%s", addr)
		serverDone <- server.ListenAndServe()
	}()

	select {
	case err := <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
}

func webFile(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, webFiles, "web/"+name)
	}
}
