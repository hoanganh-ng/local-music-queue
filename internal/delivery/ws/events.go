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
