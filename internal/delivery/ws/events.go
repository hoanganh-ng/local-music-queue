package ws

import (
	"local-music-queue/internal/domain/entity"
)

// Event type constants
const (
	EventFullSync              = "full_sync"
	EventUserJoined            = "user_joined"
	EventSongAdded             = "song_added"
	EventSongSkipped           = "song_skipped"
	EventStatusChanged         = "status_changed"
	EventElapsedSync           = "elapsed_sync"
	EventSongPrevious          = "song_previous"
	EventSongRemoved           = "song_removed"
	EventQueueCleared          = "queue_cleared"
	EventVolumeChanged         = "volume_changed"
	EventSongPrioritized       = "song_prioritized"
	EventPriorityBalanceUpdated = "priority_balance_updated"
	EventVoteUpdated           = "vote_updated"
	EventVoteResolved          = "vote_resolved"
	EventAutoQueueAdded        = "auto_queue_added"
	EventAutoQueueConfigChanged = "auto_queue_config_changed"
)

// UserJoinedData contains only the new user info
type UserJoinedData struct {
	DisplayName string          `json:"display_name"`
	Role        string          `json:"role"`
	Activity    entity.Activity `json:"activity"`
}

// SongAddedData contains only the new song
type SongAddedData struct {
	Song     entity.Song     `json:"song"`
	Position int             `json:"position"`
	Activity entity.Activity `json:"activity"`
}

// SongSkippedData contains index changes and new current song
type SongSkippedData struct {
	PreviousIndex int                   `json:"previous_index"`
	NewIndex      int                   `json:"new_index"`
	CurrentSong   *entity.Song          `json:"current_song"`
	Status        entity.PlaybackStatus `json:"status"`
	Elapsed       int                   `json:"elapsed"`
	Activity      entity.Activity       `json:"activity"`
}

// StatusChangedData contains only status and elapsed time
type StatusChangedData struct {
	Status   entity.PlaybackStatus `json:"status"`
	Elapsed  int                   `json:"elapsed"`
	Activity entity.Activity       `json:"activity"`
}

// FullSyncData contains complete state for initial sync
type FullSyncData struct {
	State *entity.Queue `json:"state"`
}

// ElapsedSyncData contains the updated elapsed time without activity log
type ElapsedSyncData struct {
	Elapsed int `json:"elapsed"`
}

// SongPreviousData contains index changes for previous song
type SongPreviousData struct {
	PreviousIndex int                   `json:"previous_index"`
	NewIndex      int                   `json:"new_index"`
	CurrentSong   *entity.Song          `json:"current_song"`
	Status        entity.PlaybackStatus `json:"status"`
	Elapsed       int                   `json:"elapsed"`
	Activity      entity.Activity       `json:"activity"`
}

// SongRemovedData contains info about removed song
type SongRemovedData struct {
	RemovedIndex int             `json:"removed_index"`
	NewIndex     int             `json:"new_index"`
	Status       entity.PlaybackStatus `json:"status"`
	Activity     entity.Activity `json:"activity"`
}

// QueueClearedData signals queue has been cleared
type QueueClearedData struct {
	Status   entity.PlaybackStatus `json:"status"`
	Activity entity.Activity       `json:"activity"`
}

// VolumeChangedData contains volume change direction
type VolumeChangedData struct {
	Direction string `json:"direction"`
}

// SongPrioritizedData contains info about prioritized song
type SongPrioritizedData struct {
	FromIndex   int             `json:"from_index"`
	ToIndex     int             `json:"to_index"`
	Song        entity.Song     `json:"song"`
	UserID      int             `json:"user_id"`
	UserBalance int             `json:"user_balance"`
	Activity    entity.Activity `json:"activity"`
}

// PriorityBalanceUpdatedData contains updated priority balance
type PriorityBalanceUpdatedData struct {
	UserID  int `json:"user_id"`
	Balance int `json:"balance"`
}

// VoteUpdatedData is broadcast after every vote cast so clients can
// show live vote counts without polling.
type VoteUpdatedData struct {
	Session  *entity.VoteSession `json:"session"`
	Activity entity.Activity     `json:"activity"`
}

// VoteResolvedData is broadcast when a session passes or expires.
type VoteResolvedData struct {
	SessionID string          `json:"session_id"`
	Outcome   string          `json:"outcome"` // "passed" | "expired"
	Activity  entity.Activity `json:"activity"`
}

// AutoQueueAddedData contains info about auto-added song.
type AutoQueueAddedData struct {
	Song            entity.Song     `json:"song"`
	SourceSongTitle string          `json:"source_song_title"`
	Activity        entity.Activity `json:"activity"`
}

// AutoQueueConfigChangedData contains the updated auto-queue configuration.
type AutoQueueConfigChangedData struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"`
}
