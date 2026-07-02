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
	// R09c: additive on top of R07b/R07d/R09a/R09b. Fires on successful
	// POST /api/rooms/{slug}/playback/volume. Direction is one of
	// "up" / "down". The global /ws 16-event inventory is unchanged;
	// this event rides /ws/rooms/{slug} only.
	EventRoomPlaybackVolumeChanged = "room_playback_volume_changed"
	// R09d: additive on top of R07b/R07d/R09a/R09b/R09c. Fires on
	// successful POST /api/rooms/{slug}/playback/prev. The payload
	// carries the previous/new indexes, the new current song, the
	// post-mutation status and elapsed, plus the authoritative queue
	// state snapshot. The global /ws 16-event inventory is unchanged;
	// this event rides /ws/rooms/{slug} only.
	EventRoomPlaybackSongPrevious = "room_playback_song_previous"
	// R09f: additive on top of R07b/R07d/R09a/R09b/R09c/R09d. Both
	// events ride /ws/rooms/{slug} ONLY. The global /ws 16-event
	// inventory is unchanged — these never appear on the global /ws
	// endpoint. EventRoomAutoQueueAdded fires after a successful
	// per-room auto-queue insertion; EventRoomAutoQueueConfigChanged
	// fires after a successful per-room toggle (host/admin only).
	EventRoomAutoQueueAdded         = "room_auto_queue_added"
	EventRoomAutoQueueConfigChanged = "room_auto_queue_config_changed"
	// R10b: additive on top of R07b/R07d/R09a/R09b/R09c/R09d/R09f.
	// EventRoomMemberRemoved is delivered to the removed user's per-room
	// WS connections BEFORE the server closes them. The wire value is
	// "room_member_removed". Only rides /ws/rooms/{slug}; the global
	// /ws 16-event inventory is unchanged.
	EventRoomMemberRemoved = "room_member_removed"
	// EventRoomMembersChanged is delivered to the remaining per-room WS
	// clients after a successful member removal. The wire value is
	// "room_members_changed". Only rides /ws/rooms/{slug}; the global
	// /ws 16-event inventory is unchanged.
	EventRoomMembersChanged = "room_members_changed"
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

// RoomVoteSessionDTO is the wire-shape of an in-memory room vote
// session for /ws/rooms/{slug} payloads. It deliberately omits
// entity.VoteSession.VotedBy (the per-user ballot) so the broadcast
// surface never leaks which userIDs voted. It mirrors the fields
// clients need to render a live vote pill: id, type, songID,
// songTitle, songIndex, voteCount, threshold, createdAt, expiresAt,
// remainingSeconds.
type RoomVoteSessionDTO struct {
	ID               string    `json:"id"`
	Type             string    `json:"type"`
	SongID           string    `json:"song_id"`
	SongTitle        string    `json:"song_title"`
	SongIndex        int       `json:"song_index"`
	VoteCount        int       `json:"vote_count"`
	Threshold        int       `json:"threshold"`
	CreatedAt        time.Time `json:"created_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	RemainingSeconds int       `json:"remaining_seconds"`
}

// newRoomVoteSessionDTO sanitises an entity.VoteSession for wire
// transmission. Never returns nil; pass-through when the input is nil
// so the marshaller always emits the field shape consistently.
func newRoomVoteSessionDTO(s *entity.VoteSession) *RoomVoteSessionDTO {
	if s == nil {
		return nil
	}
	return &RoomVoteSessionDTO{
		ID:               s.ID,
		Type:             string(s.Type),
		SongID:           s.SongID,
		SongTitle:        s.SongTitle,
		SongIndex:        s.SongIndex,
		VoteCount:        s.VoteCount(),
		Threshold:        s.Threshold,
		CreatedAt:        s.CreatedAt,
		ExpiresAt:        s.ExpiresAt,
		RemainingSeconds: s.RemainingSeconds(),
	}
}

// RoomVoteUpdatedData is broadcast after every successful vote cast at
// POST /api/rooms/{slug}/vote/skip. R09b addition; carries the
// post-cast session snapshot (sanitised via RoomVoteSessionDTO — note
// that VotedBy / actor ballots are intentionally NEVER serialised on
// this surface) so clients can render live vote counts without
// polling, plus the server-resolved actor_user_id for attribution and
// the authoritative post-mutation queue state.
//
// Fields are intentionally kept flat to mirror the global
// EventVoteUpdated shape, except Session now points at the DTO so
// the per-user ballot never crosses the wire.
type RoomVoteUpdatedData struct {
	RoomSlug    string                `json:"room_slug"`
	Session     *RoomVoteSessionDTO   `json:"session"`
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

// RoomPlaybackVolumeChangedData is broadcast after a successful
// POST /api/rooms/{slug}/playback/volume. R09c addition; the global
// /ws contract is unchanged. The payload carries only the slug and
// direction — there is no persisted volume state, so no authoritative
// state snapshot is included. Clients that want a stable UI can
// optimistically adjust their local display.
type RoomPlaybackVolumeChangedData struct {
	RoomSlug  string `json:"room_slug"`
	Direction string `json:"direction"`
}

// RoomPlaybackSongPreviousData is broadcast after a successful
// POST /api/rooms/{slug}/playback/prev. R09d addition; carries the
// prev/next indexes, the new current song, the post-mutation status
// and elapsed, and the authoritative queue state snapshot so clients
// do not need a follow-up sync. The 16-event global /ws inventory is
// unchanged; this event rides /ws/rooms/{slug} only.
type RoomPlaybackSongPreviousData struct {
	RoomSlug      string                `json:"room_slug"`
	PreviousIndex int                   `json:"previous_index"`
	NewIndex      int                   `json:"new_index"`
	CurrentSong   *entity.Song          `json:"current_song"`
	Status        entity.PlaybackStatus `json:"status"`
	Elapsed       int                   `json:"elapsed"`
	State         *entity.Queue         `json:"state"`
}

// RoomAutoQueueAddedData is broadcast after a successful per-room
// auto-queue insertion. R09f addition; rides /ws/rooms/{slug} only.
// The payload carries the auto-added song, the source song's title,
// the authoritative post-mutation snapshot (current_index,
// current_song, status, elapsed), and the full queue state so
// clients do not need a follow-up sync. The 16-event global /ws
// inventory is unchanged — the global auto_queue_added event
// continues to live solely on the global /ws endpoint and is never
// reused here.
type RoomAutoQueueAddedData struct {
	RoomSlug        string                `json:"room_slug"`
	Song            entity.Song           `json:"song"`
	SourceSongTitle string                `json:"source_song_title"`
	CurrentIndex    int                   `json:"current_index"`
	CurrentSong     *entity.Song          `json:"current_song"`
	Status          entity.PlaybackStatus `json:"status"`
	Elapsed         int                   `json:"elapsed"`
	State           *entity.Queue         `json:"state"`
}

// RoomAutoQueueConfigChangedData is broadcast after a successful
// per-room auto-queue toggle. R09f addition; rides /ws/rooms/{slug}
// only. The envelope mirrors the global AutoQueueConfigChangedData
// shape but carries room_slug so subscribers can route the event to
// the correct per-room store slice. The 16-event global /ws inventory
// is unchanged — this event is purely additive.
type RoomAutoQueueConfigChangedData struct {
	RoomSlug string `json:"room_slug"`
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"`
}

// RoomMemberRemovedData is the targeted payload delivered to the
// removed user before the server closes their per-room WS
// connections. R10b addition; purely additive — the global /ws
// 16-event inventory is unchanged.
type RoomMemberRemovedData struct {
	RoomSlug string `json:"room_slug"`
	UserID   int    `json:"user_id"`
	Reason   string `json:"reason"` // "host_removed"
}

// RoomMemberInfo is the per-member entry in the room_members_changed
// payload. The entity has a JoinedAt field, but the wire shape
// intentionally OMITS it (per R10a Decision 9) so the envelope is
// small — clients needing joined_at can refetch via
// GET /api/rooms/{slug}/members.
type RoomMemberInfo struct {
	UserID int                   `json:"user_id"`
	Role   entity.RoomMemberRole `json:"role"`
}

// RoomMembersChangedData is broadcast to the remaining per-room WS
// clients after a successful member removal. R10b addition; rides
// /ws/rooms/{slug} only.
type RoomMembersChangedData struct {
	RoomSlug string           `json:"room_slug"`
	Members  []RoomMemberInfo `json:"members"`
}
