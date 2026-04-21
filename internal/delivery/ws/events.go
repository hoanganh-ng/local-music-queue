package ws

import (
	"local-music-queue/internal/domain/entity"
)

// Event type constants
const (
	EventFullSync      = "full_sync"
	EventUserJoined    = "user_joined"
	EventSongAdded     = "song_added"
	EventSongSkipped   = "song_skipped"
	EventStatusChanged = "status_changed"
	EventElapsedSync   = "elapsed_sync"
	EventSongPrevious  = "song_previous"
	EventSongRemoved   = "song_removed"
	EventQueueCleared  = "queue_cleared"
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
