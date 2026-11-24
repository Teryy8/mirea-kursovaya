package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStatsPersistAfterReopeningDatabase(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test data", "analytics #1.db")
	start := func() (*eventStore, http.Handler) {
		t.Helper()
		store, err := openEventStore(context.Background(), filename)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.db.Close() })
		return store, newMux(store)
	}

	first, handler := start()
	send := func(body string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/events", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("POST /api/events returned %d, want %d: %s", res.Code, want, res.Body)
		}
	}
	send(`{"type":"page_view","path":"/demo/about"}`, http.StatusCreated)
	send(`{"type":"page_view","path":"/demo/catalog"}`, http.StatusCreated)
	send(`{"type":"page_view","path":"/demo/catalog"}`, http.StatusCreated)
	send(`{"type":"page_view","path":"/demo/catalog?foo=bar"}`, http.StatusBadRequest)

	if err := first.db.Close(); err != nil {
		t.Fatal(err)
	}
	send(`{"type":"page_view","path":"/demo/"}`, http.StatusServiceUnavailable)

	_, handler = start()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("GET /api/stats returned %d: %s", res.Code, res.Body)
	}
	var got stats
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := stats{TotalViews: 3, Pages: []pageViews{
		{Path: "/demo/catalog", Views: 2},
		{Path: "/demo/about", Views: 1},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statistics after reopening: got %+v, want %+v", got, want)
	}
}
