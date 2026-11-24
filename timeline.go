package main

import (
	"context"
	"net/http"
	"time"
)

type minuteViews struct {
	Minute time.Time `json:"minute"`
	Views  uint64    `json:"views"`
}

type sourceViews struct {
	Domain string `json:"domain"`
	Views  uint64 `json:"views"`
}

type recentEvent struct {
	Type       string    `json:"type"`
	Path       string    `json:"path"`
	ReceivedAt time.Time `json:"received_at"`
}

type timeline struct {
	Minutes        int           `json:"minutes"`
	Points         []minuteViews `json:"points"`
	Sources        []sourceViews `json:"sources"`
	ActiveSessions uint64        `json:"active_sessions"`
	Recent         []recentEvent `json:"recent"`
}

func (a *analytics) timeline(w http.ResponseWriter, r *http.Request) {
	minutes := 15
	switch r.URL.Query().Get("minutes") {
	case "", "15":
	case "60":
		minutes = 60
	default:
		writeError(w, http.StatusBadRequest, "minutes must be 15 or 60")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), databaseTimeout)
	defer cancel()
	snapshot, err := a.store.readTimeline(ctx, time.Now(), minutes)
	if err != nil {
		databaseError(w, "read live analytics", err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *eventStore) readTimeline(ctx context.Context, now time.Time, minutes int) (timeline, error) {
	// Include the current, still incomplete minute.
	start := now.UTC().Truncate(time.Minute).Add(-time.Duration(minutes-1) * time.Minute)
	from, until := start.UnixMilli(), now.UnixMilli()+1
	result := timeline{Minutes: minutes}
	var err error
	result.Points, err = s.viewsByMinute(ctx, start, until, minutes)
	if err != nil {
		return timeline{}, err
	}
	result.Sources, err = s.viewsBySource(ctx, from, until)
	if err != nil {
		return timeline{}, err
	}
	result.Recent, err = s.recentEvents(ctx, from, until)
	if err != nil {
		return timeline{}, err
	}
	const active = `
		SELECT COUNT(DISTINCT d.session_id)
		FROM events e JOIN event_details d ON d.event_row = e.id
		WHERE e.processed_at IS NOT NULL AND e.received_at >= ? AND e.received_at < ?
		  AND d.session_id != ''`
	err = s.db.QueryRowContext(ctx, active, now.Add(-5*time.Minute).UnixMilli(), until).Scan(&result.ActiveSessions)
	return result, err
}

func (s *eventStore) viewsByMinute(ctx context.Context, start time.Time, until int64, minutes int) ([]minuteViews, error) {
	const query = `
		SELECT (received_at - ?) / 60000 AS bucket, COUNT(*)
		FROM events
		WHERE processed_at IS NOT NULL AND event_type = 'page_view'
		  AND received_at >= ? AND received_at < ?
		GROUP BY bucket`
	rows, err := s.db.QueryContext(ctx, query, start.UnixMilli(), start.UnixMilli(), until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	points := make([]minuteViews, minutes)
	for i := range points {
		points[i].Minute = start.Add(time.Duration(i) * time.Minute)
	}
	for rows.Next() {
		var bucket int
		var views uint64
		if err := rows.Scan(&bucket, &views); err != nil {
			return nil, err
		}
		points[bucket].Views = views
	}
	return points, rows.Err()
}

func (s *eventStore) viewsBySource(ctx context.Context, from, until int64) ([]sourceViews, error) {
	const query = `
		SELECT COALESCE(d.referrer, '') AS domain, COUNT(*) AS views
		FROM events e LEFT JOIN event_details d ON d.event_row = e.id
		WHERE e.processed_at IS NOT NULL AND e.event_type = 'page_view'
		  AND e.received_at >= ? AND e.received_at < ?
		GROUP BY domain ORDER BY views DESC, domain LIMIT 10`
	rows, err := s.db.QueryContext(ctx, query, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []sourceViews{}
	for rows.Next() {
		var source sourceViews
		if err := rows.Scan(&source.Domain, &source.Views); err != nil {
			return nil, err
		}
		result = append(result, source)
	}
	return result, rows.Err()
}

func (s *eventStore) recentEvents(ctx context.Context, from, until int64) ([]recentEvent, error) {
	const query = `
		SELECT event_type, path, received_at FROM events
		WHERE processed_at IS NOT NULL AND event_type != 'heartbeat'
		  AND received_at >= ? AND received_at < ?
		ORDER BY received_at DESC, id DESC LIMIT 10`
	rows, err := s.db.QueryContext(ctx, query, from, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []recentEvent{}
	for rows.Next() {
		var item recentEvent
		var receivedAt int64
		if err := rows.Scan(&item.Type, &item.Path, &receivedAt); err != nil {
			return nil, err
		}
		item.ReceivedAt = time.UnixMilli(receivedAt).UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}
