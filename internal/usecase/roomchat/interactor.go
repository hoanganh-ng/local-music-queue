// Package roomchat hosts the per-room chat use case (R11a). It
// owns validation (trim, CRLF normalize, length cap), active-room
// gating, active-membership gating, persistence of plain-text
// messages, and the per-room broadcast of the post-mutation message
// after a successful persistence. Sender identity comes from the
// bearer/session actor; room identity comes from the slug resolved
// against RoomRepository. The package is independent of delivery/ws
// (the broadcaster seam is an interface declared here, satisfied by
// *ws.RoomWSHub via a thin adapter in cmd/server).
//
// Locking: this use case has no coordinator mutex — every operation
// is a single-row insert or a bounded SELECT against an indexed
// (room_id, created_at) pair. The broadcast is fire-and-forget on
// the broadcaster seam, mirroring the existing roomqueue /
// roomautoqueue patterns.
//
// Persistence: messages are persisted for the lifetime of the room;
// R11a does NOT introduce a retention purge job. Future lifecycle
// hardening (R10f+) may add retention windows.
package roomchat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"local-music-queue/internal/domain/entity"
)

// Sentinel errors mapped to HTTP status codes by the handler layer.
var (
	// ErrInvalidLimit is returned by ListRecent when the requested limit
	// is < 1 or > the documented cap (100). The handler maps this to 400.
	ErrInvalidLimit = errors.New("invalid chat message limit")

	// ErrEmptyContent / ErrContentTooLong are forwarded from
	// entity.NormalizeChatContent so the handler maps them to 400.
	// They are re-exported as package vars so callers (and tests) can
	// use errors.Is without depending on entity.
	ErrEmptyContent  = entity.ErrEmptyChatContent
	ErrContentTooLong = entity.ErrChatContentTooLong

	// ErrInvalidSlug is returned when the slug fails entity.IsValidSlug.
	// The handler maps this to 400 — distinct from ErrRoomNotFound
	// (404) so the wire status code can match the documented R11a
	// status mapping.
	ErrInvalidSlug = errors.New("invalid room slug")

	// ErrRoomNotFound / ErrArchived / ErrForbidden come from the room
	// resolution + membership gate; the handler maps them to 404/409/403
	// (the same way roomqueue does).
	ErrRoomNotFound = errors.New("room not found")
	ErrArchived     = errors.New("room archived")
	ErrForbidden    = errors.New("forbidden")

	// ErrSenderNotFound is returned when UserRepository.GetUserByID
	// returns sql.ErrNoRows for the resolved sender. This is a 500
	// failure mode (the session is valid but the users row has
	// vanished) and is not expected in production. It is exposed so
	// usecase tests can pin the failure handling.
	ErrSenderNotFound = errors.New("sender not found")
)

// DefaultChatHistoryLimit is the documented default page size for
// ListRecent when the caller does not specify a limit.
const DefaultChatHistoryLimit = 50

// MaxChatHistoryLimit caps the per-call page size for ListRecent.
// Callers MUST reject limit > MaxChatHistoryLimit (the handler maps
// that to 400).
const MaxChatHistoryLimit = 100

// Broadcaster is implemented by *ws.RoomWSHub (via a thin adapter in
// cmd/server) to publish the room_chat_message_created envelope after
// a successful persistence. Nil-safe: the interactor skips the
// broadcast when the seam is unset. The adapter resolves the sender
// display name; the seam only carries the slug + persisted message so
// the broadcaster stays free of any user-repo dependency.
type Broadcaster interface {
	BroadcastRoomChatMessageCreated(roomSlug string, msg *entity.RoomChatMessage, displayName string)
}

// ChatMessageRepository is the narrow subset of
// repository.RoomChatMessageRepository the interactor needs. The
// full interface is implemented by PostgresRoomChatMessageRepository;
// tests inject a smaller fake via this seam.
type ChatMessageRepository interface {
	CreateMessage(ctx context.Context, roomID int64, senderID int, content string, now time.Time) (*entity.RoomChatMessage, error)
	ListRecentMessages(ctx context.Context, roomID int64, limit int) ([]entity.RoomChatMessage, error)
}

// RoomLookup is the narrow subset of repository.RoomRepository the
// interactor needs (slug → room, and membership gate). The full
// interface is implemented by PostgresRoomRepository; tests inject a
// smaller fake via this seam.
type RoomLookup interface {
	GetRoomBySlug(ctx context.Context, slug string) (*entity.Room, error)
	GetMember(ctx context.Context, roomID int64, userID int) (*entity.RoomMember, error)
}

// UserLookup is the narrow subset of repository.UserRepository the
// interactor needs (sender display-name resolution). The full
// interface is implemented by PostgresUserRepository; tests inject a
// smaller fake via this seam.
type UserLookup interface {
	GetUserByID(ctx context.Context, id int) (*entity.User, error)
}

// Interactor owns the per-room chat use case.
type Interactor struct {
	repo      ChatMessageRepository
	roomRepo  RoomLookup
	userRepo  UserLookup
	now       func() time.Time
	broadcast Broadcaster // nil-safe; set via SetBroadcaster
}

// NewInteractor constructs an Interactor with the chat, room, and
// user repositories. The clock defaults to time.Now; tests may swap
// it via SetClock. The broadcaster is optional — see SetBroadcaster.
func NewInteractor(chatRepo ChatMessageRepository, roomRepo RoomLookup, userRepo UserLookup) *Interactor {
	return &Interactor{repo: chatRepo, roomRepo: roomRepo, userRepo: userRepo, now: time.Now}
}

// SetBroadcaster wires the per-room chat broadcaster seam. Optional —
// when unset the interactor still succeeds; it just doesn't broadcast.
func (i *Interactor) SetBroadcaster(b Broadcaster) { i.broadcast = b }

// SetClock replaces the time source (tests only).
func (i *Interactor) SetClock(now func() time.Time) { i.now = now }

// ChatMessageWithSender pairs a persisted message with the sender's
// resolved display name. The HTTP layer renders the
// display_name field (or the documented "user #<id>" fallback when
// the user row's display_name is empty) and never exposes the email.
type ChatMessageWithSender struct {
	Message     *entity.RoomChatMessage
	DisplayName string
}

// Send validates and persists a plain-text chat message. It returns
// the freshly persisted row paired with the resolved sender display
// name so the HTTP layer can render the post-mutation envelope.
//
// Sender identity MUST come from the actorUserID argument (the
// bearer/session user id resolved by main.go's roomAuth wrapper).
// The actorUserID is never read from any request field.
//
// On success AND when the broadcaster seam is wired, the interactor
// invokes BroadcastRoomChatMessageCreated exactly once. A failed
// broadcast is not surfaced (the message is already persisted and
// the next GET will surface it to clients); a logger-style seam
// could be added in a future sprint.
func (i *Interactor) Send(ctx context.Context, slug string, actorUserID int, rawContent string) (*ChatMessageWithSender, error) {
	if actorUserID == 0 {
		return nil, ErrForbidden
	}
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	content, err := entity.NormalizeChatContent(rawContent)
	if err != nil {
		// Surface the canonical sentinel without wrapping so the
		// handler can map via errors.Is.
		return nil, err
	}
	room, err := i.resolveActiveRoom(ctx, slug, actorUserID)
	if err != nil {
		return nil, err
	}
	now := i.now()
	msg, err := i.repo.CreateMessage(ctx, room.ID, actorUserID, content, now)
	if err != nil {
		return nil, fmt.Errorf("create chat message: %w", err)
	}
	display, derr := i.resolveDisplayName(ctx, actorUserID)
	if derr != nil {
		return nil, derr
	}
	if i.broadcast != nil {
		i.broadcast.BroadcastRoomChatMessageCreated(room.Slug, msg, display)
	}
	return &ChatMessageWithSender{Message: msg, DisplayName: display}, nil
}

// ListRecent returns up to `limit` most recent messages for an active
// room, oldest → newest, paired with the resolved sender display
// name per row. The caller (HTTP handler) MUST clamp limit to
// 1..MaxChatHistoryLimit before calling; passing 0 is rejected with
// ErrInvalidLimit so the default is owned by the handler.
func (i *Interactor) ListRecent(ctx context.Context, slug string, actorUserID int, limit int) ([]ChatMessageWithSender, error) {
	if actorUserID == 0 {
		return nil, ErrForbidden
	}
	if !entity.IsValidSlug(slug) {
		return nil, ErrInvalidSlug
	}
	if limit < 1 || limit > MaxChatHistoryLimit {
		return nil, ErrInvalidLimit
	}
	room, err := i.resolveActiveRoom(ctx, slug, actorUserID)
	if err != nil {
		return nil, err
	}
	rows, err := i.repo.ListRecentMessages(ctx, room.ID, limit)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	out := make([]ChatMessageWithSender, 0, len(rows))
	// Display names are resolved per-sender; cache the lookup so a
	// history burst from a single sender (R11a typical case) doesn't
	// hammer the user repo. Cache is local to the call — no shared
	// state, no locking.
	displayCache := make(map[int]string, len(rows))
	for idx := range rows {
		m := &rows[idx]
		name, ok := displayCache[m.SenderID]
		if !ok {
			n, err := i.resolveDisplayName(ctx, m.SenderID)
			if err != nil {
				return nil, err
			}
			name = n
			displayCache[m.SenderID] = name
		}
		out = append(out, ChatMessageWithSender{Message: m, DisplayName: name})
	}
	return out, nil
}

// resolveActiveRoom validates the slug, fetches the room, gates
// active-room status, and gates active-membership for the actor.
// Returns ErrRoomNotFound / ErrArchived / ErrForbidden as documented.
func (i *Interactor) resolveActiveRoom(ctx context.Context, slug string, actorUserID int) (*entity.Room, error) {
	room, err := i.roomRepo.GetRoomBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoomNotFound
		}
		return nil, fmt.Errorf("get room: %w", err)
	}
	if room.Status != entity.RoomStatusActive {
		return nil, ErrArchived
	}
	if _, err := i.roomRepo.GetMember(ctx, room.ID, actorUserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrForbidden
		}
		return nil, fmt.Errorf("get member: %w", err)
	}
	return room, nil
}

// resolveDisplayName looks up the sender's display_name; if the
// user row is missing it returns ErrSenderNotFound (which the
// caller surfaces as a 500); if the user's display_name is empty it
// falls back to "user #<id>" so the wire never carries an empty
// string and richer sender profiles are deferred to a future
// sprint.
func (i *Interactor) resolveDisplayName(ctx context.Context, senderID int) (string, error) {
	u, err := i.userRepo.GetUserByID(ctx, senderID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrSenderNotFound
		}
		return "", fmt.Errorf("get user: %w", err)
	}
	name := strings.TrimSpace(u.DisplayName)
	if name == "" {
		name = fmt.Sprintf("user #%d", senderID)
	}
	return name, nil
}