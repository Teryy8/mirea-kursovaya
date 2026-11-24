package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
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
	mode := "all"
	if len(os.Args) == 2 {
		mode = os.Args[1]
	}
	switch mode {
	case "all", "server", "worker", "backup", "restore":
	default:
		log.Fatal("usage: mirea-kursovaya [server|worker|backup|restore]")
	}
	if len(os.Args) > 2 {
		log.Fatal("usage: mirea-kursovaya [server|worker|backup|restore]")
	}
	if err := run(mode); err != nil {
		log.Fatal(err)
	}
}

func run(mode string) error {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data/analytics.db"
	}
	if mode == "backup" || mode == "restore" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if mode == "backup" {
			return backupDatabase(ctx, dbPath, os.Stdout)
		}
		return restoreDatabase(ctx, dbPath, os.Stdin)
	}

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), databaseTimeout)
	store, err := openEventStore(startupCtx, dbPath)
	cancelStartup()
	if err != nil {
		return fmt.Errorf("open database %q: %w", dbPath, err)
	}
	defer store.db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if mode == "worker" {
		log.Printf("LiveTraffic worker: %s", dbPath)
		store.runWorker(ctx)
		return nil
	}
	access, err := loadDashboardAccess()
	if err != nil {
		return fmt.Errorf("configure dashboard access: %w", err)
	}
	if mode == "all" {
		workerCtx, cancel := context.WithCancel(ctx)
		workerDone := make(chan struct{})
		go func() {
			defer close(workerDone)
			store.runWorker(workerCtx)
		}()
		defer func() {
			cancel()
			<-workerDone
		}()
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           newMuxWithAccess(store, access),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serverDone := make(chan error, 1)
	go func() {
		log.Printf("LiveTraffic: http://%s", addr)
		serverDone <- server.ListenAndServe()
	}()

	select {
	case err := <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}

func newMux(store *eventStore) *http.ServeMux {
	return newMuxWithAccess(store, nil)
}

func newMuxWithAccess(store *eventStore, access *dashboardAccess) *http.ServeMux {
	api := analytics{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", api.health)
	mux.HandleFunc("POST /api/events", api.addEvent)
	mux.HandleFunc("GET /api/stats", access.protect(api.stats))
	mux.HandleFunc("GET /api/timeseries", access.protect(api.timeline))
	mux.HandleFunc("GET /{$}", access.protect(webFile("dashboard.html")))
	mux.HandleFunc("GET /demo/{$}", webFile("demo.html"))
	mux.HandleFunc("GET /demo/catalog", webFile("demo.html"))
	mux.HandleFunc("GET /demo/about", webFile("demo.html"))
	mux.HandleFunc("GET /styles.css", webFile("styles.css"))
	mux.HandleFunc("GET /dashboard.js", webFile("dashboard.js"))
	mux.HandleFunc("GET /tracker.js", webFile("tracker.js"))
	return mux
}

func webFile(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, webFiles, "web/"+name)
	}
}
