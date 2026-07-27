package roomcutover

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/infrastructure/persistence"
)

// newCutoverTestDB returns a per-test *sql.DB scoped to a throwaway PG schema
// migrated to the current version (9). It skips when PG is unreachable.
func newCutoverTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := persistence.NewRoomTestDB(t)
	return db
}

// mustJSON marshals v or fails the test.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// fixedClock returns an Options.Now that always yields the same UTC instant.
func fixedClock() func() time.Time {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return func() time.Time { return at }
}

// seedUser inserts a user and returns its generated id.
func seedUser(t *testing.T, db *sql.DB, email string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO users (email, display_name, role, priority_balance)
		VALUES ($1, $2, 'user', 0)
		RETURNING id
	`, email, email).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

// seedQueueState writes the single legacy queue_state row from a canonical
// entity.Queue.
func seedQueueState(t *testing.T, db *sql.DB, q entity.Queue) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO queue_state (id, data) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data
	`, mustJSON(t, q)); err != nil {
		t.Fatalf("seed queue_state: %v", err)
	}
}

// seedActivities inserts legacy activities with explicit timestamps.
func seedActivities(t *testing.T, db *sql.DB, acts []entity.Activity) {
	t.Helper()
	for _, a := range acts {
		if _, err := db.ExecContext(context.Background(), `
			INSERT INTO activities ("timestamp", type, "user", description)
			VALUES ($1, $2, $3, $4)
		`, a.Timestamp, string(a.Type), a.User, a.Description); err != nil {
			t.Fatalf("seed activity: %v", err)
		}
	}
}

// setAutoQueueConfig updates the seeded legacy auto_queue_config row.
func setAutoQueueConfig(t *testing.T, db *sql.DB, enabled bool, strategy string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		UPDATE auto_queue_config SET enabled = $1, strategy = $2 WHERE id = 1
	`, enabled, strategy); err != nil {
		t.Fatalf("set auto_queue_config: %v", err)
	}
}

type playRow struct {
	videoID  string
	title    string
	playedAt time.Time
}

// seedPlayHistory inserts legacy play_history rows.
func seedPlayHistory(t *testing.T, db *sql.DB, rows []playRow) {
	t.Helper()
	for _, r := range rows {
		if _, err := db.ExecContext(context.Background(), `
			INSERT INTO play_history (video_id, title, played_at)
			VALUES ($1, $2, $3)
		`, r.videoID, r.title, r.playedAt); err != nil {
			t.Fatalf("seed play_history: %v", err)
		}
	}
}

// seedForeignRoomPlayHistory creates an unrelated room and pushes n rows into
// its room_play_history so the cutover's play_history offset is forced above
// zero. Returns the highest room_play_history id written.
func seedForeignRoomPlayHistory(t *testing.T, db *sql.DB, n int) int64 {
	t.Helper()
	var roomID int64
	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO rooms (slug, name) VALUES ('other-room', 'Other') RETURNING id
	`).Scan(&roomID); err != nil {
		t.Fatalf("seed foreign room: %v", err)
	}
	var maxID int64
	for i := 0; i < n; i++ {
		if err := db.QueryRowContext(context.Background(), `
			INSERT INTO room_play_history (room_id, video_id, title)
			VALUES ($1, $2, $3) RETURNING id
		`, roomID, "foreign", "Foreign").Scan(&maxID); err != nil {
			t.Fatalf("seed foreign room_play_history: %v", err)
		}
	}
	return maxID
}

// countInt runs a single-value integer query.
func countInt(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

// sampleQueue builds a small valid non-empty queue with history for fixtures.
func sampleQueue() entity.Queue {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return entity.Queue{
		Songs: []entity.Song{
			{ID: "vid1", Title: "Song One", URL: "https://y/1", AddedBy: "alice", AddedByID: 1},
			{ID: "vid2", Title: "Song Two", URL: "https://y/2", AddedBy: "bob", AddedByID: 2},
		},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      15,
		History: []entity.Activity{
			{Timestamp: ts, Type: entity.ActivitySongAdded, User: "alice", Description: "added Song One"},
		},
	}
}

// seedTypicalLegacyState seeds a representative legacy global state and returns
// the host user id.
func seedTypicalLegacyState(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	hostID := seedUser(t, db, "host@example.test")
	seedQueueState(t, db, sampleQueue())
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedActivities(t, db, []entity.Activity{
		{Timestamp: base, Type: entity.ActivitySongAdded, User: "alice", Description: "added one"},
		{Timestamp: base.Add(time.Minute), Type: entity.ActivityUserJoined, User: "bob", Description: "joined"},
	})
	setAutoQueueConfig(t, db, true, "related")
	seedPlayHistory(t, db, []playRow{
		{videoID: "vid1", title: "Song One", playedAt: base},
		{videoID: "vid2", title: "Song Two", playedAt: base.Add(2 * time.Minute)},
	})
	return hostID
}

// baseOptions returns options with a fixed clock and injected build SHA.
func baseOptions(hostID int64) Options {
	return Options{
		RoomSlug:   "legacy-room",
		RoomName:   "Legacy Room",
		HostUserID: hostID,
		Now:        fixedClock(),
	}
}
