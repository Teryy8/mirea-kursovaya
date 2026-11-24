package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"regexp"
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
	ID        string `json:"event_id,omitempty"`
	Type      string `json:"type"`
	Path      string `json:"path"`
	SessionID string `json:"session_id,omitempty"`
	Referrer  string `json:"referrer,omitempty"`
}

var referrerDomain = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

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

	switch e.Type {
	case "page_view", "click", "heartbeat":
	default:
		writeError(w, http.StatusBadRequest, "type must be page_view, click or heartbeat")
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
	if e.SessionID != "" {
		id, err := uuid.Parse(e.SessionID)
		if err != nil || len(e.SessionID) != 36 {
			writeError(w, http.StatusBadRequest, "session_id must be a UUID")
			return
		}
		e.SessionID = id.String()
	}
	if e.Type == "heartbeat" && e.SessionID == "" {
		writeError(w, http.StatusBadRequest, "heartbeat requires a session_id")
		return
	}
	e.Referrer = strings.ToLower(strings.TrimSuffix(e.Referrer, "."))
	if e.Referrer != "" && (e.Type != "page_view" || len(e.Referrer) > 253 ||
		!referrerDomain.MatchString(e.Referrer) || e.Referrer[strings.LastIndexByte(e.Referrer, '.')+1] < 'a') {
		writeError(w, http.StatusBadRequest, "referrer must be a domain for a page view")
		return
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
	writeError(w, http.StatusBadRequest, "expected one JSON event object")
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
