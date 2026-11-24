package main

import (
	"context"
	"log"
	"time"
)

const (
	workerInterval = 250 * time.Millisecond
	batchSize      = 128
)

func (s *eventStore) runWorker(ctx context.Context) {
	ticker := time.NewTicker(workerInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		batchCtx, cancel := context.WithTimeout(ctx, databaseTimeout)
		processed, err := s.processPending(batchCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("process events: %v", err)
		}
		if err == nil && processed == batchSize {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *eventStore) processPending(ctx context.Context) (int64, error) {
	var pending bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM events WHERE processed_at IS NULL)").Scan(&pending)
	if err != nil || !pending {
		return 0, err
	}

	// The immediate transaction reserves the write lock. Both statements
	// therefore see the same ordered batch across worker processes.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	const batch = `SELECT id, path, event_type FROM events WHERE processed_at IS NULL ORDER BY id LIMIT ?`
	const countViews = `
		INSERT INTO page_view_counts (path, views)
		SELECT path, COUNT(*) FROM (` + batch + `)
		WHERE event_type = 'page_view'
		GROUP BY path
		ON CONFLICT(path) DO UPDATE SET views = page_view_counts.views + excluded.views`
	if _, err := tx.ExecContext(ctx, countViews, batchSize); err != nil {
		return 0, err
	}

	const markDone = `
		UPDATE events SET processed_at = ?
		WHERE id IN (SELECT id FROM (` + batch + `))`
	result, err := tx.ExecContext(ctx, markDone, time.Now().UnixMilli(), batchSize)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}
