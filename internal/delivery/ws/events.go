package ws

import (
	"time"

	"local-music-queue/internal/domain/entity"
)

// Event type constants
const (
	EventFullSync               = "full_sync"
	EventUserJoined             = "user_joined"
	EventSongAdded              = "song_added"
	EventSongSkipped            = "song_skipped"
	EventStatusChanged          = "status_changed"
	EventElapsedSync            = "elapsed_sync"
	EventSongPrevious           = "song_previous"
	EventSongRemoved            = "song_removed"
	EventQueueCleared           = "queue_cleared"
	EventVolumeChanged          = "volume_changed"
	EventSongPrioritized        = "song_prioritized"
	EventPriorityBalanceUpdated = "priority_balance_updated"
	EventVoteUpdated            = "vote_updated"
	EventVoteResolved           = "vote_resolved"
	EventAutoQueueAdded         = "auto_queue_added"
	EventAutoQueueConfigChanged = "auto_queue_config_changed"
	// EventError is sent to a client when the server rejects an inbound
	// action (e.g. unauthenticated request_full_sync, role mismatch on a
	// future privileged client message). R05 addition; purely additive.
	EventError = "error"
	// EventRoomArchived is sent when a room transitions to archived because
	// the player lease expired past grace or the host explicitly released.
	// R06 addition; purely additive. Clients treat it as the redirect
	// trigger to Welcome.
	EventRoomArchived = "room_archived"
	// Room queue events (R07b). Purely additive — the 16-event backend
	// application inventory and the room_archived envelope from R06 are
	// unchanged. Sequencing is per-room, single-process, in-memory.
	EventRoomQueueSync        = "room_queue_sync"
	EventRoomQueueSongAdded   = "room_queue_song_added"
	EventRoomQueueSongRemoved = "room_queue_song_removed"
	EventRoomQueueCleared     = "room_queue_cleared"
	// EventRoomQueueSongPrioritized is broadcast after a successful POST
	// /api/rooms/{slug}/queue/prioritize. R07d addition; existing R07b
	// contracts are unchanged.
	EventRoomQueueSongPrioritized = "room_queue_song_prioritized"
	// Room playback events (R09a). Purely additive on top of R07b/R07d.
	// Only the active lease holder may trigger these via REST; the
	// resulting events ride the per-room WebSocket only — the global
	// /ws 16-event inventory is unchanged.
	EventRoomPlaybackStatusChanged = "room_playback_status_changed"
	EventRoomPlaybackElapsedSync   = "room_playback_elapsed_sync"
	EventRoomPlaybackSongAdvanced  = "room_playback_song_advanced"
	// Room vote events (R09b). Purely additive on top of R07b/R07d/R09a.
	// Only ride /ws/rooms/{slug}; the global /ws 16-event inventory is
	// unchanged. Sessions are room-scoped, current-song-scoped, in-memory,
	// single-instance only, never persisted.
	EventRoomVoteUpdated  = "room_vote_updated"
	EventRoomVoteResolved = "room_vote_resolved"
)

// UserJoinedData contains only the new user info
type UserJoinedData struct {
	DisplayName string          `json:"display_name"`
	Role        string          `json:"role"`
	Activity    entity.Activity `json:"activity"`
}

// SongAddedData contains only the new song.
// Sprint 004 adds the authoritative post-mutation snapshot
// (current_index, current_song, status, elapsed) so clients no longer need
// to infer playback state after receiving the event.
type SongAddedData struct {
	Song         entity.Song           `json:"song"`
	Position     int                   `json:"position"`
	Activity     entity.Activity       `json:"activity"`
	CurrentIndex int                   `json:"current_index"`
	CurrentSong  *entity.Song          `json:"current_song"`
	Status       entity.PlaybackStatus `json:"status"`
	Elapsed      int                   `json:"elapsed"`
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
	RemovedIndex int                   `json:"removed_index"`
	NewIndex     int                   `json:"new_index"`
	Status       entity.PlaybackStatus `json:"status"`
	Activity     entity.Activity       `json:"activity"`
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
	Session     *entity.VoteSession `json:"session"`
	Activity    entity.Activity     `json:"activity"`
	InitialSync bool                `json:"initial_sync,omitempty"`
}

// VoteResolvedData is broadcast when a session passes or expires.
type VoteResolvedData struct {
	SessionID string          `json:"session_id"`
	Outcome   string          `json:"outcome"` // "passed" | "expired"
	Activity  entity.Activity `json:"activity"`
}

// AutoQueueAddedData contains info about auto-added song.
// Sprint 004 adds the authoritative post-mutation snapshot
// (current_index, current_song, status, elapsed) so clients no longer need
// to infer playback state after receiving the event.
type AutoQueueAddedData struct {
	Song            entity.Song           `json:"song"`
	SourceSongTitle string                `json:"source_song_title"`
	Activity        entity.Activity       `json:"activity"`
	CurrentIndex    int                   `json:"current_index"`
	CurrentSong     *entity.Song          `json:"current_song"`
	Status          entity.PlaybackStatus `json:"status"`
	Elapsed         int                   `json:"elapsed"`
}

// Client-to-server message type constants.
const (
	// ClientMsgRequestFullSync is sent by a client that detected a sequence
	// gap and needs a fresh authoritative snapshot.
	ClientMsgRequestFullSync = "request_full_sync"
)

// AutoQueueConfigChangedData contains the updated auto-queue configuration.
type AutoQueueConfigChangedData struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"`
}

// ClientMessage is the envelope used to decode any message arriving from a
// WebSocket client. Only the Type field is required; additional fields are
// message-type-specific and decoded separately.
type ClientMessage struct {
	Type string `json:"type"`
}

// ErrorData is sent to a client when the server rejects an inbound action.
// R05 introduces this envelope so unauthorized WebSocket attempts receive a
// structured rejection instead of being silently dropped. Pre-existing 16
// event types are unchanged; EventError is purely additive.
type ErrorData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RoomArchivedData describes an archived room event payload.
// R06 addition; purely additive. The 16 pre-existing event types and
// their JSON tags remain byte-for-byte compatible.
type RoomArchivedData struct {
	RoomID     int64     `json:"room_id"`
	Reason     string    `json:"reason"`
	ArchivedAt time.Time `json:"archived_at"`
}

// RoomQueueSyncData is the initial snapshot a room client receives on
// connect. Mirrors FullSyncData but is scoped to a single room and uses
// its own envelope name so clients can route room events independently
// of the global full_sync.
type RoomQueueSyncData struct {
	RoomSlug string        `json:"room_slug"`
	State    *entity.Queue `json:"state"`
}

// RoomQueueSongAddedData is broadcast after a successful POST
// /api/rooms/{slug}/queue/add. Carries the post-mutation snapshot so
// clients can render the new queue without a follow-up sync.
type RoomQueueSongAddedData struct {
	RoomSlug string        `json:"room_slug"`
	Song     entity.Song   `json:"song"`
	Position int           `json:"position"`
	State    *entity.Queue `json:"state"`
}

// RoomQueueSongRemovedData is broadcast after a successful POST
// /api/rooms/{slug}/queue/remove. The post-mutation snapshot is
// authoritative; clients do not need to re-fetch.
type RoomQueueSongRemovedData struct {
	RoomSlug     string        `json:"room_slug"`
	RemovedIndex int           `json:"removed_index"`
	State        *entity.Queue `json:"state"`
}

// RoomQueueClearedData is broadcast after a successful POST
// /api/rooms/{slug}/queue/clear.
type RoomQueueClearedData struct {
	RoomSlug string        `json:"room_slug"`
	State    *entity.Queue `json:"state"`
}

// RoomQueueSongPrioritizedData is broadcast after a successful POST
// /api/rooms/{slug}/queue/prioritize. The post-mutation snapshot is
// authoritative; clients do not need to re-fetch. from_index is the
// pre-mutation slot; to_index is the post-mutation slot (always
// CurrentIndex + 1 per the entity invariant).
type RoomQueueSongPrioritizedData struct {
	RoomSlug  string        `json:"room_slug"`
	FromIndex int           `json:"from_index"`
	ToIndex   int           `json:"to_index"`
	Song      entity.Song   `json:"song"`
	State     *entity.Queue `json:"state"`
}

// RoomPlaybackStatusChangedData is broadcast after a successful POST
// /api/rooms/{slug}/playback/status. R09a addition; existing R07
// contracts and the global 16-event inventory are unchanged. The
// post-mutation snapshot is authoritative.
type RoomPlaybackStatusChangedData struct {
	RoomSlug string                `json:"room_slug"`
	Status   entity.PlaybackStatus `json:"status"`
	Elapsed  int                   `json:"elapsed"`
	State    *entity.Queue         `json:"state"`
}

// RoomPlaybackElapsedSyncData is broadcast after a successful POST
// /api/rooms/{slug}/playback/sync. R09a addition; carries the new
// elapsed value plus the post-mutation snapshot.
type RoomPlaybackElapsedSyncData struct {
	RoomSlug string        `json:"room_slug"`
	Elapsed  int           `json:"elapsed"`
	State    *entity.Queue `json:"state"`
}

// RoomPlaybackSongAdvancedData is broadcast after a successful POST
// /api/rooms/{slug}/playback/{skip,ended}. R09a addition; reason is
// "skip" or "ended" so clients can render the appropriate UX (skip
// keeps playing; ended on the last song pauses). The post-mutation
// snapshot is authoritative.
type RoomPlaybackSongAdvancedData struct {
	RoomSlug      string                `json:"room_slug"`
	Reason        string                `json:"reason"`
	PreviousIndex int                   `json:"previous_index"`
	NewIndex      int                   `json:"new_index"`
	CurrentSong   *entity.Song          `json:"current_song"`
	Status        entity.PlaybackStatus `json:"status"`
	Elapsed       int                   `json:"elapsed"`
	State         *entity.Queue         `json:"state"`
}

// RoomVoteUpdatedData is broadcast after every successful vote cast at
// POST /api/rooms/{slug}/vote/skip. R09b addition; carries the
// post-cast session snapshot so clients can render live vote counts
// without polling, plus the server-resolved actor_user_id for
// attribution and the authoritative post-mutation queue state.
//
// The session snapshot itself is JSON-serialisable (it already exposes
// VotedBy / Threshold / Expiry fields). Fields are intentionally kept
// flat to mirror the global EventVoteUpdated shape.
type RoomVoteUpdatedData struct {
	RoomSlug    string                `json:"room_slug"`
	Session     *entity.VoteSession   `json:"session"`
	ActorUserID int                   `json:"actor_user_id"`
	State       *entity.Queue         `json:"state"`
}

// RoomVoteResolvedData is broadcast when a vote session is deleted
// (either because the threshold was reached and we advanced, or
// because it expired without passing). R09b addition; carried on
// /ws/rooms/{slug} only.
//
// When Outcome == "passed", the existing room_playback_song_advanced
// event is also broadcast with reason="skip" and carries the
// authoritative post-mutation state. When Outcome == "expired", the
// state is the unchanged queue.
type RoomVoteResolvedData struct {
	RoomSlug  string        `json:"room_slug"`
	SessionID string        `json:"session_id"`
	Outcome   string        `json:"outcome"` // "passed" | "expired"
	State     *entity.Queue `json:"state"`
}
