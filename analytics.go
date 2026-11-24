package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const databaseTimeout = 5 * time.Second

type analytics struct {
	store *eventStore
}

type event struct {
	ID   string `json:"event_id,omitempty"`
	Type string `json:"type"`
	Path string `json:"path"`
}

type pageViews struct {
	Path  string `json:"path"`
	Views uint64 `json:"views"`
}

type stats struct {
	TotalViews uint64      `json:"total_views"`
	Pages      []pageViews `json:"pages"`
}

func (a *analytics) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), databaseTimeout)
	defer cancel()
	if err := a.store.db.PingContext(ctx); err != nil {
		databaseError(w, "check database connection", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *analytics) addEvent(w http.ResponseWriter, r *http.Request) {
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	var e event
	if err := decoder.Decode(&e); err != nil {
		writeDecodeError(w, err)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeDecodeError(w, err)
		return
	}

	if e.Type != "page_view" {
		writeError(w, http.StatusBadRequest, "type must be page_view")
		return
	}
	if e.ID != "" {
		id, err := uuid.Parse(e.ID)
		if err != nil || len(e.ID) != 36 {
			writeError(w, http.StatusBadRequest, "event_id must be a UUID")
			return
		}
		e.ID = id.String()
	}
	if !strings.HasPrefix(e.Path, "/") || strings.HasPrefix(e.Path, "//") ||
		len(e.Path) > 256 || strings.ContainsAny(e.Path, "?#\\") ||
		strings.ContainsFunc(e.Path, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		writeError(w, http.StatusBadRequest, "path must be a local page path without query or fragment")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), databaseTimeout)
	defer cancel()
	result, err := a.store.enqueue(ctx, e)
	switch {
	case err != nil:
		databaseError(w, "queue event", err)
	case result == eventConflict:
		writeError(w, http.StatusConflict, "event_id already belongs to a different event")
	case result == eventDuplicate:
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
	default:
		writeJSON(w, http.StatusAccepted, e)
	}
}

func (a *analytics) stats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), databaseTimeout)
	defer cancel()
	snapshot, err := a.store.pageStats(ctx)
	if err != nil {
		databaseError(w, "read page statistics", err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func databaseError(w http.ResponseWriter, operation string, err error) {
	log.Printf("%s: %v", operation, err)
	writeError(w, http.StatusServiceUnavailable, "database unavailable")
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds 4096 bytes")
		return
	}
	writeError(w, http.StatusBadRequest, "expected one JSON object with type, path and optional event_id")
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
