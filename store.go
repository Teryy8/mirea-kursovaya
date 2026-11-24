package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const initialSchema = `
CREATE TABLE IF NOT EXISTS events (
	id           INTEGER PRIMARY KEY,
	event_type   TEXT NOT NULL,
	path         TEXT NOT NULL,
	received_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS events_type_path ON events (event_type, path);
`

const migration1 = `
ALTER TABLE events ADD COLUMN event_id TEXT;
ALTER TABLE events ADD COLUMN processed_at INTEGER;

UPDATE events SET processed_at = received_at;
CREATE UNIQUE INDEX events_event_id ON events (event_id);
CREATE INDEX events_pending ON events (id) WHERE processed_at IS NULL;

CREATE TABLE page_view_counts (
	path  TEXT PRIMARY KEY,
	views INTEGER NOT NULL
);
INSERT INTO page_view_counts (path, views)
	SELECT path, COUNT(*) FROM events WHERE event_type = 'page_view' GROUP BY path;
PRAGMA user_version = 1;
`

type eventStore struct {
	db *sql.DB
}

func openEventStore(ctx context.Context, filename string) (*eventStore, error) {
	path, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}

	// A file URI escapes special characters in DB_PATH. The driver applies
	// busy_timeout to each connection it opens.
	dsn := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(path),
		RawQuery: url.Values{
			"_pragma": {"busy_timeout(3000)"},
			"_txlock": {"immediate"},
		}.Encode(),
	}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	// Each process opens one connection to the local SQLite file.
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize event schema: %w", err)
	}
	return &eventStore{db: db}, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	switch version {
	case 0:
		if _, err := tx.ExecContext(ctx, initialSchema+migration1); err != nil {
			return err
		}
	case 1:
	default:
		return fmt.Errorf("unsupported schema version %d", version)
	}
	return tx.Commit()
}

type enqueueResult int

const (
	eventQueued enqueueResult = iota
	eventDuplicate
	eventConflict
)

func (s *eventStore) enqueue(ctx context.Context, e event) (enqueueResult, error) {
	const insert = `
		INSERT INTO events (event_type, path, received_at, event_id)
		VALUES (?, ?, ?, NULLIF(?, ''))
		ON CONFLICT(event_id) DO NOTHING`
	result, err := s.db.ExecContext(ctx, insert, e.Type, e.Path, time.Now().UnixMilli(), e.ID)
	if err != nil {
		return eventQueued, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 1 {
		return eventQueued, err
	}

	var savedType, savedPath string
	err = s.db.QueryRowContext(ctx, "SELECT event_type, path FROM events WHERE event_id = ?", e.ID).
		Scan(&savedType, &savedPath)
	if err != nil {
		return eventQueued, err
	}
	if savedType != e.Type || savedPath != e.Path {
		return eventConflict, nil
	}
	return eventDuplicate, nil
}

func (s *eventStore) pageStats(ctx context.Context) (stats, error) {
	const query = `SELECT path, views FROM page_view_counts ORDER BY views DESC, path ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return stats{}, err
	}
	defer rows.Close()

	snapshot := stats{Pages: []pageViews{}}
	for rows.Next() {
		var page pageViews
		if err := rows.Scan(&page.Path, &page.Views); err != nil {
			return stats{}, err
		}
		snapshot.Pages = append(snapshot.Pages, page)
		snapshot.TotalViews += page.Views
	}
	if err := rows.Err(); err != nil {
		return stats{}, err
	}
	return snapshot, nil
}
