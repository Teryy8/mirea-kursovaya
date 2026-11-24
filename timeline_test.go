package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTimelinePeriodsAndEventTypes(t *testing.T) {
	ctx := context.Background()
	s, err := openEventStore(ctx, filepath.Join(t.TempDir(), "timeline.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	now := time.Date(2026, 11, 24, 12, 0, 35, 0, time.UTC)
	for _, item := range []struct {
		at                    time.Time
		kind, session, domain string
		processed             bool
	}{
		{now.Add(-25 * time.Minute), "page_view", "expired", "archive.example", true},
		{now.UTC().Truncate(time.Minute).Add(-14 * time.Minute), "page_view", "old-tab", "", true},
		{now.Add(-time.Minute), "page_view", "tab-1", "example.com", true},
		{now, "page_view", "tab-1", "example.com", true},
		{now, "click", "tab-1", "", true},
		{now, "heartbeat", "tab-2", "", true},
		{now, "page_view", "pending", "pending.example", false},
		{now.Add(time.Second), "page_view", "future", "future.example", true},
	} {
		var processed any
		if item.processed {
			processed = item.at.UnixMilli()
		}
		result, err := s.db.ExecContext(ctx, "INSERT INTO events (event_type,path,received_at,processed_at) VALUES (?, '/demo/', ?, ?)",
			item.kind, item.at.UnixMilli(), processed)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, "INSERT INTO event_details VALUES (?, ?, ?)", id, item.session, item.domain); err != nil {
			t.Fatal(err)
		}
	}

	short, err := s.readTimeline(ctx, now, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(short.Points) != 15 || short.Points[0].Views != 1 || short.Points[13].Views != 1 || short.Points[14].Views != 1 ||
		short.Points[12].Views != 0 || !short.Points[14].Minute.Equal(now.Truncate(time.Minute)) {
		t.Fatalf("unexpected minute buckets: %+v", short.Points)
	}
	if short.ActiveSessions != 2 || len(short.Recent) != 4 || short.Recent[0].Type != "click" {
		t.Fatalf("unexpected recent events or active sessions: %+v", short)
	}
	if !reflect.DeepEqual(short.Sources, []sourceViews{{Domain: "example.com", Views: 2}, {Views: 1}}) {
		t.Fatalf("sources: %+v", short.Sources)
	}

	hour, err := s.readTimeline(ctx, now, 60)
	if err != nil || len(hour.Points) != 60 || len(hour.Sources) != 3 || hour.ActiveSessions != 2 {
		t.Fatalf("hour timeline: %+v, error %v", hour, err)
	}

	response := httptest.NewRecorder()
	newMux(s).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/timeseries?minutes=17", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid period: got HTTP %d", response.Code)
	}
}
