package domain

import (
	"context"
	"local-music-queue/internal/domain/entity"
	"time"
)

// AutoQueueConfig holds the persisted settings for radio mode.
type AutoQueueConfig struct {
	Enabled  bool
	Strategy AutoQueueStrategy
}

type AutoQueueStrategy string

const (
	StrategyRelated       AutoQueueStrategy = "related"
	StrategyHistoryRandom AutoQueueStrategy = "history_random"
)

// PlayHistoryEntry is a record of a song that has been played.
type PlayHistoryEntry struct {
	VideoID  string
	Title    string
	PlayedAt time.Time
}

// AutoQueueRepository persists config and play history.
type AutoQueueRepository interface {
	GetConfig(ctx context.Context) (*AutoQueueConfig, error)
	SaveConfig(ctx context.Context, cfg AutoQueueConfig) error

	AppendHistory(ctx context.Context, entry PlayHistoryEntry) error
	// GetRecentHistory returns up to `limit` entries, newest first.
	GetRecentHistory(ctx context.Context, limit int) ([]PlayHistoryEntry, error)
}

// RelatedSongFetcher fetches one candidate song related to a given video.
// exclude is a list of videoIDs already in queue or recently played.
type RelatedSongFetcher interface {
	FetchRelated(ctx context.Context, videoID string, exclude []string) (*entity.Song, error)
}
