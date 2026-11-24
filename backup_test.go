package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupRestoresCountsAndPendingEvents(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "active.db")
	store, err := openEventStore(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()

	processed := event{ID: "1", Type: "page_view", Path: "/demo/"}
	pending := event{ID: "2", Type: "page_view", Path: "/demo/catalog"}
	if _, err := store.enqueue(ctx, processed); err != nil {
		t.Fatal(err)
	}
	if n, err := store.processPending(ctx); n != 1 || err != nil {
		t.Fatalf("preparing source: processed %d, err %v", n, err)
	}
	if _, err := store.enqueue(ctx, pending); err != nil {
		t.Fatal(err)
	}
	var snapshot bytes.Buffer
	if err := backupDatabase(ctx, source, &snapshot); err != nil {
		t.Fatal(err)
	}
	if n, err := store.processPending(ctx); n != 1 || err != nil {
		t.Fatalf("updating source after backup: processed %d, err %v", n, err)
	}

	restoredPath := filepath.Join(dir, "restored #1.db")
	if err := restoreDatabase(ctx, restoredPath, bytes.NewReader(snapshot.Bytes())); err != nil {
		t.Fatal(err)
	}
	if err := restoreDatabase(ctx, restoredPath, bytes.NewReader(snapshot.Bytes())); err == nil {
		t.Fatal("expected restore to refuse to overwrite the destination")
	}
	restored, err := openEventStore(ctx, restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.db.Close()

	assertStats(t, newMux(restored), stats{TotalViews: 1, Pages: []pageViews{{Path: "/demo/", Views: 1}}})
	if result, err := restored.enqueue(ctx, processed); result != eventDuplicate || err != nil {
		t.Fatalf("deduplicate restored event: result %v, err %v", result, err)
	}
	if n, err := restored.processPending(ctx); n != 1 || err != nil {
		t.Fatalf("process restored queue: processed %d, err %v", n, err)
	}
	assertStats(t, newMux(restored), stats{TotalViews: 2, Pages: []pageViews{
		{Path: "/demo/", Views: 1},
		{Path: "/demo/catalog", Views: 1},
	}})
}

func TestRestoreRejectsInvalidBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "analytics.db")
	if err := restoreDatabase(context.Background(), path, bytes.NewBufferString("not sqlite")); err == nil {
		t.Fatal("expected an invalid backup to be rejected")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unverified database was published: %v", err)
	}
}
