package roomcutover

import (
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
)

func TestValidateQueue(t *testing.T) {
	tests := []struct {
		name    string
		queue   entity.Queue
		wantErr bool
	}{
		{
			name:  "empty idle is valid",
			queue: entity.Queue{Songs: []entity.Song{}, CurrentIndex: -1, Status: entity.StatusIdle, Elapsed: 0},
		},
		{
			name: "non-empty playing is valid",
			queue: entity.Queue{
				Songs:        []entity.Song{{ID: "a", Title: "A", URL: "u"}},
				CurrentIndex: 0,
				Status:       entity.StatusPlaying,
				Elapsed:      12,
			},
		},
		{
			name:    "unknown status",
			queue:   entity.Queue{Songs: []entity.Song{}, CurrentIndex: -1, Status: "bogus"},
			wantErr: true,
		},
		{
			name:    "negative elapsed",
			queue:   entity.Queue{Songs: []entity.Song{{ID: "a", Title: "A", URL: "u"}}, CurrentIndex: 0, Status: entity.StatusPlaying, Elapsed: -1},
			wantErr: true,
		},
		{
			name:    "empty with non -1 index",
			queue:   entity.Queue{Songs: []entity.Song{}, CurrentIndex: 0, Status: entity.StatusIdle},
			wantErr: true,
		},
		{
			name:    "empty but not idle",
			queue:   entity.Queue{Songs: []entity.Song{}, CurrentIndex: -1, Status: entity.StatusPlaying},
			wantErr: true,
		},
		{
			name:    "non-empty index out of range",
			queue:   entity.Queue{Songs: []entity.Song{{ID: "a", Title: "A", URL: "u"}}, CurrentIndex: 5, Status: entity.StatusPlaying},
			wantErr: true,
		},
		{
			name:    "non-empty idle",
			queue:   entity.Queue{Songs: []entity.Song{{ID: "a", Title: "A", URL: "u"}}, CurrentIndex: 0, Status: entity.StatusIdle},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateQueue(&tc.queue)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCanonicalizeQueueRejectsInvalid(t *testing.T) {
	if _, err := canonicalizeQueue([]byte(`{not json`)); err == nil {
		t.Fatal("expected decode error")
	}
	// Valid JSON but violates the empty/idle invariant.
	if _, err := canonicalizeQueue([]byte(`{"songs":[],"current_index":3,"status":"idle","elapsed":0,"history":[]}`)); err == nil {
		t.Fatal("expected invariant error")
	}
}

// TestHashQueueJSONIsFormatIndependent proves that two byte-different but
// logically identical queue documents (reordered keys, whitespace, and a
// timestamp expressed in a non-UTC zone) hash to the same value. This is the
// property that lets the TEXT source and JSONB target compare equal.
func TestHashQueueJSONIsFormatIndependent(t *testing.T) {
	// Canonical form via the entity type.
	loc := time.FixedZone("UTC+5", 5*3600)
	ts := time.Date(2026, 1, 2, 8, 4, 5, 0, loc) // 03:04:05Z
	q := entity.Queue{
		Songs:        []entity.Song{{ID: "vid1", Title: "Song One", URL: "https://x/1"}},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
		Elapsed:      30,
		History: []entity.Activity{
			{Timestamp: ts, Type: entity.ActivitySongAdded, User: "alice", Description: "added Song One"},
		},
	}
	canonical := mustJSON(t, q)

	// A hand-written variant: keys in a different order, extra whitespace, and
	// the same instant written as an explicit UTC offset.
	variant := []byte(`{
		"status": "playing",
		"elapsed": 30,
		"current_index": 0,
		"history": [
			{"user":"alice","description":"added Song One","type":"song_added","timestamp":"2026-01-02T03:04:05Z"}
		],
		"songs": [
			{"id":"vid1","title":"Song One","artist":"","duration":0,"thumbnail":"","url":"https://x/1","added_by":"","added_by_id":0,"is_prioritized":false}
		]
	}`)

	h1, err := hashQueueJSON(canonical)
	if err != nil {
		t.Fatalf("hash canonical: %v", err)
	}
	h2, err := hashQueueJSON(variant)
	if err != nil {
		t.Fatalf("hash variant: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("expected identical hashes, got %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex sha256, got %d chars", len(h1))
	}
}

func TestHashesEqual(t *testing.T) {
	a := map[string]string{"x": "1", "y": "2"}
	if !hashesEqual(a, map[string]string{"x": "1", "y": "2"}) {
		t.Fatal("expected equal")
	}
	if hashesEqual(a, map[string]string{"x": "1"}) {
		t.Fatal("different length should not be equal")
	}
	if hashesEqual(a, map[string]string{"x": "1", "y": "3"}) {
		t.Fatal("different value should not be equal")
	}
}
