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

const schema = `
CREATE TABLE IF NOT EXISTS events (
	id           INTEGER PRIMARY KEY,
	event_type   TEXT NOT NULL,
	path         TEXT NOT NULL,
	received_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS events_type_path ON events (event_type, path);
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
		Scheme:   "file",
		Path:     filepath.ToSlash(path),
		RawQuery: url.Values{"_pragma": {"busy_timeout(3000)"}}.Encode(),
	}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	// Keep reads and writes sequential on this single-process stand.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize event schema: %w", err)
	}
	return &eventStore{db: db}, nil
}

func (s *eventStore) insertEvent(ctx context.Context, e event) error {
	const query = `INSERT INTO events (event_type, path, received_at) VALUES (?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, e.Type, e.Path, time.Now().UnixMilli())
	return err
}

func (s *eventStore) pageStats(ctx context.Context) (stats, error) {
	const query = `
		SELECT path, COUNT(*) AS views
		FROM events
		WHERE event_type = ?
		GROUP BY path
		ORDER BY views DESC, path ASC`
	rows, err := s.db.QueryContext(ctx, query, "page_view")
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
