package main

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type analytics struct {
	mu    sync.Mutex
	total uint64
	views map[string]uint64
}

type event struct {
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

func (a *analytics) health(w http.ResponseWriter, _ *http.Request) {
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
	if !strings.HasPrefix(e.Path, "/") || strings.HasPrefix(e.Path, "//") ||
		len(e.Path) > 256 || strings.ContainsAny(e.Path, "?#\\") ||
		strings.ContainsFunc(e.Path, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		writeError(w, http.StatusBadRequest, "path must be a local page path without query or fragment")
		return
	}

	a.mu.Lock()
	a.total++
	a.views[e.Path]++
	a.mu.Unlock()
	writeJSON(w, http.StatusCreated, e)
}

func (a *analytics) stats(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	snapshot := stats{TotalViews: a.total, Pages: make([]pageViews, 0, len(a.views))}
	for path, views := range a.views {
		snapshot.Pages = append(snapshot.Pages, pageViews{Path: path, Views: views})
	}
	a.mu.Unlock()

	sort.Slice(snapshot.Pages, func(i, j int) bool {
		left, right := snapshot.Pages[i], snapshot.Pages[j]
		if left.Views == right.Views {
			return left.Path < right.Path
		}
		return left.Views > right.Views
	})
	writeJSON(w, http.StatusOK, snapshot)
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body exceeds 4096 bytes")
		return
	}
	writeError(w, http.StatusBadRequest, "expected one JSON object with type and path")
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
