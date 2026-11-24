package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStatsPersistAfterReopeningDatabase(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "test data", "analytics #1.db")
	seedLegacyDatabase(t, filename)

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
	const view = `{"event_id":"e2ca4f68-cc59-46da-a5b3-ec10a96e1120","type":"page_view","path":"/demo/catalog"}`
	send(view, http.StatusAccepted)
	send(view, http.StatusOK)
	send(`{"event_id":"e2ca4f68-cc59-46da-a5b3-ec10a96e1120","type":"page_view","path":"/demo/about"}`, http.StatusConflict)
	send(`{"event_id":"bad-id","type":"page_view","path":"/demo/about"}`, http.StatusBadRequest)
	send(`{"type":"page_view","path":"/demo/catalog"}`, http.StatusAccepted)
	send(`{"type":"page_view","path":"/demo/catalog?foo=bar"}`, http.StatusBadRequest)
	assertStats(t, handler, stats{TotalViews: 1, Pages: []pageViews{{Path: "/demo/about", Views: 1}}})

	if err := first.db.Close(); err != nil {
		t.Fatal(err)
	}
	send(`{"type":"page_view","path":"/demo/"}`, http.StatusServiceUnavailable)

	worker, handler := start()
	if n, err := worker.processPending(context.Background()); n != 2 || err != nil {
		t.Fatalf("process after reopening: processed %d, error %v", n, err)
	}
	send(view, http.StatusOK)
	if n, err := worker.processPending(context.Background()); n != 0 || err != nil {
		t.Fatalf("process duplicate: processed %d, error %v", n, err)
	}
	assertStats(t, handler, stats{TotalViews: 3, Pages: []pageViews{
		{Path: "/demo/catalog", Views: 2},
		{Path: "/demo/about", Views: 1},
	}})
}

func seedLegacyDatabase(t *testing.T, filename string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(initialSchema + `
		INSERT INTO events (event_type, path, received_at)
		VALUES ('page_view', '/demo/about', 1000);`); err != nil {
		t.Fatal(err)
	}
}

func assertStats(t *testing.T, handler http.Handler, want stats) {
	t.Helper()
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("GET /api/stats returned %d: %s", res.Code, res.Body)
	}
	var got stats
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("statistics: got %+v, want %+v", got, want)
	}
}

func TestWorkerRollsBackCountsOnFailure(t *testing.T) {
	ctx := context.Background()
	store, err := openEventStore(ctx, filepath.Join(t.TempDir(), "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	if _, err := store.enqueue(ctx, event{Type: "page_view", Path: "/demo/"}); err != nil {
		t.Fatal(err)
	}
	// Force an error in the second write, after the worker has incremented the counter.
	if _, err := store.db.Exec(`CREATE TRIGGER fail_update BEFORE UPDATE OF processed_at ON events
		BEGIN SELECT RAISE(ABORT, 'simulated failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.processPending(ctx); err == nil {
		t.Fatal("expected event processing to fail")
	}
	assertStats(t, newMux(store), stats{Pages: []pageViews{}})
	if _, err := store.db.Exec("DROP TRIGGER fail_update"); err != nil {
		t.Fatal(err)
	}
	if n, err := store.processPending(ctx); n != 1 || err != nil {
		t.Fatalf("process after recovery: processed %d, error %v", n, err)
	}
	assertStats(t, newMux(store), stats{TotalViews: 1, Pages: []pageViews{{Path: "/demo/", Views: 1}}})
}
