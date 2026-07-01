package domain

import (
	"context"
	"time"
)

// RoomAutoQueueConfig is the persisted per-room auto-queue (radio mode)
// setting. R09f addition: the config is room-scoped and is intentionally
// independent from the global AutoQueueConfig so toggling the global
// setting MUST NOT mutate a per-room setting (and vice versa).
//
// A missing row for roomID resolves to the documented default
// (Enabled=false, Strategy=StrategyRelated) at the repository layer —
// mirrors the global auto_queue_config behavior in
// internal/domain/auto_queue.go.
type RoomAutoQueueConfig struct {
	Enabled  bool
	Strategy AutoQueueStrategy
}

// RoomPlayHistoryEntry is a record of a song that has finished playing
// in a specific room. The 50-row per-room cap is enforced server-side
// inside AppendHistory (no SQL trigger), mirroring the global
// play_history cap from R01/R02.
type RoomPlayHistoryEntry struct {
	VideoID  string
	Title    string
	PlayedAt time.Time
}

// RoomAutoQueueRepository persists per-room auto-queue config and
// per-room play history. Implementations are expected to be safe for
// concurrent use; the interactor layer additionally serializes
// triggers under a coordinator mutex.
//
// The repository is keyed by roomID (NOT slug) — slug resolution is the
// responsibility of the caller (roomqueue / roomautoqueue Interactor),
// which already holds the room row under its own mutex.
type RoomAutoQueueRepository interface {
	// GetConfig returns the auto-queue config for roomID. A missing row
	// resolves to the documented default (Enabled=false,
	// Strategy=StrategyRelated) so callers never see a nil config.
	GetConfig(ctx context.Context, roomID int64) (*RoomAutoQueueConfig, error)

	// SaveConfig upserts the config for roomID.
	SaveConfig(ctx context.Context, roomID int64, cfg RoomAutoQueueConfig) error

	// AppendHistory inserts a row in room_play_history and enforces the
	// per-room 50-row cap by deleting the oldest excess rows in the
	// same transaction. The cap mirrors the global play_history cap.
	AppendHistory(ctx context.Context, roomID int64, entry RoomPlayHistoryEntry) error

	// GetRecentHistory returns up to `limit` entries for roomID,
	// newest first.
	GetRecentHistory(ctx context.Context, roomID int64, limit int) ([]RoomPlayHistoryEntry, error)
}
