package roomchat

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"local-music-queue/internal/domain/entity"
)

// fakeChatRepo is the in-memory stand-in for
// repository.RoomChatMessageRepository. It records every persist
// call so tests can assert the broadcast-vs-no-broadcast invariant.
type fakeChatRepo struct {
	mu       sync.Mutex
	nextID   int64
	messages map[int64][]entity.RoomChatMessage // keyed by room_id
	createCalls int
	createErr   error
}

func newFakeChatRepo() *fakeChatRepo {
	return &fakeChatRepo{messages: map[int64][]entity.RoomChatMessage{}}
}

func (r *fakeChatRepo) CreateMessage(_ context.Context, roomID int64, senderID int, content string, now time.Time) (*entity.RoomChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	if r.createErr != nil {
		return nil, r.createErr
	}
	r.nextID++
	msg := entity.RoomChatMessage{
		ID:        r.nextID,
		RoomID:    roomID,
		SenderID:  senderID,
		Content:   content,
		CreatedAt: now,
	}
	r.messages[roomID] = append(r.messages[roomID], msg)
	return &msg, nil
}

func (r *fakeChatRepo) ListRecentMessages(_ context.Context, roomID int64, limit int) ([]entity.RoomChatMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := r.messages[roomID]
	if len(rows) > limit {
		rows = rows[len(rows)-limit:]
	}
	out := make([]entity.RoomChatMessage, len(rows))
	copy(out, rows)
	return out, nil
}

func (r *fakeChatRepo) seeded(roomID int64, msgs ...entity.RoomChatMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages[roomID] = append(r.messages[roomID], msgs...)
}

// fakeRoomRepo is the in-memory stand-in for repository.RoomRepository.
// It implements only the surface the roomchat interactor touches
// (GetRoomBySlug, GetMember).
type fakeRoomRepo struct {
	mu      sync.Mutex
	rooms   map[string]*entity.Room
	members map[int64]map[int]entity.RoomMember
}

func newFakeRoomRepo() *fakeRoomRepo {
	return &fakeRoomRepo{
		rooms:   map[string]*entity.Room{},
		members: map[int64]map[int]entity.RoomMember{},
	}
}

func (r *fakeRoomRepo) putRoom(slug, name string, status entity.RoomStatus) *entity.Room {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := int64(len(r.rooms) + 1)
	rm := &entity.Room{ID: id, Slug: slug, Name: name, Status: status, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	r.rooms[slug] = rm
	r.members[id] = map[int]entity.RoomMember{}
	return rm
}

func (r *fakeRoomRepo) putMember(roomID int64, userID int, role entity.RoomMemberRole) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.members[roomID] == nil {
		r.members[roomID] = map[int]entity.RoomMember{}
	}
	r.members[roomID][userID] = entity.RoomMember{RoomID: roomID, UserID: userID, Role: role, JoinedAt: time.Now()}
}

func (r *fakeRoomRepo) GetRoomBySlug(_ context.Context, slug string) (*entity.Room, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rm, ok := r.rooms[slug]
	if !ok {
		return nil, sql.ErrNoRows
	}
	cp := *rm
	return &cp, nil
}

func (r *fakeRoomRepo) GetMember(_ context.Context, roomID int64, userID int) (*entity.RoomMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[roomID][userID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return &m, nil
}

// stubBroadcastBroadcaster records every BroadcastRoomChatMessageCreated
// call so tests can assert the no-broadcast-on-failure invariant and
// the broadcast-after-persist invariant.
type stubBroadcastBroadcaster struct {
	mu      sync.Mutex
	calls   []broadcastCall
	enabled bool
}

type broadcastCall struct {
	Slug        string
	MessageID   int64
	SenderID    int
	Content     string
	DisplayName string
}

func (b *stubBroadcastBroadcaster) BroadcastRoomChatMessageCreated(roomSlug string, msg *entity.RoomChatMessage, displayName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.enabled {
		return
	}
	b.calls = append(b.calls, broadcastCall{
		Slug:        roomSlug,
		MessageID:   msg.ID,
		SenderID:    msg.SenderID,
		Content:     msg.Content,
		DisplayName: displayName,
	})
}

// fakeUserRepo is the in-memory stand-in for repository.UserRepository.
// Only GetUserByID is exercised by the chat interactor.
type fakeUserRepo struct {
	mu    sync.Mutex
	users map[int]*entity.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{users: map[int]*entity.User{}}
}

func (r *fakeUserRepo) putUser(id int, displayName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[id] = &entity.User{ID: id, DisplayName: displayName, Email: "redacted@example.com", Role: entity.RoleGuest}
}

func (r *fakeUserRepo) GetUserByID(_ context.Context, id int) (*entity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	cp := *u
	return &cp, nil
}

// Compile-time assertions that the fakes satisfy the narrow interactor
// seams (declared in interactor.go). The full repository interfaces
// are still implemented in internal/infrastructure/persistence.
var (
	_ ChatMessageRepository = (*fakeChatRepo)(nil)
	_ RoomLookup            = (*fakeRoomRepo)(nil)
	_ UserLookup            = (*fakeUserRepo)(nil)
)

// --- helpers ---

func newTestInteractor(t *testing.T, roomRepo *fakeRoomRepo, userRepo *fakeUserRepo) (*Interactor, *fakeChatRepo, *stubBroadcastBroadcaster) {
	t.Helper()
	chatRepo := newFakeChatRepo()
	bcast := &stubBroadcastBroadcaster{enabled: true}
	inter := NewInteractor(chatRepo, roomRepo, userRepo)
	inter.SetBroadcaster(bcast)
	return inter, chatRepo, bcast
}

// --- tests ---

func TestSend_Success(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	users.putUser(42, "Alex")

	inter, chatRepo, bcast := newTestInteractor(t, rooms, users)

	out, err := inter.Send(context.Background(), "lobby", 42, "  hello  ")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
	if out == nil || out.Message == nil {
		t.Fatalf("Send: nil message")
	}
	if got, want := out.Message.Content, "hello"; got != want {
		t.Fatalf("Send: content = %q, want %q (trim should strip whitespace)", got, want)
	}
	if got, want := out.Message.SenderID, 42; got != want {
		t.Fatalf("Send: sender_id = %d, want %d", got, want)
	}
	if got, want := out.DisplayName, "Alex"; got != want {
		t.Fatalf("Send: display_name = %q, want %q", got, want)
	}
	if chatRepo.createCalls != 1 {
		t.Fatalf("Send: persist count = %d, want 1", chatRepo.createCalls)
	}
	if got := len(bcast.calls); got != 1 {
		t.Fatalf("Send: broadcast count = %d, want 1", got)
	}
	if got := bcast.calls[0]; got.Slug != "lobby" || got.DisplayName != "Alex" || got.Content != "hello" {
		t.Fatalf("Send: broadcast tuple = %+v", got)
	}
}

func TestSend_NormalizesCRLF(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	users.putUser(42, "Alex")
	inter, _, _ := newTestInteractor(t, rooms, users)

	out, err := inter.Send(context.Background(), "lobby", 42, "hello\r\nworld\ragain")
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
	if got, want := out.Message.Content, "hello\nworld\nagain"; got != want {
		t.Fatalf("Send: CRLF normalization failed; got %q want %q", got, want)
	}
}

func TestSend_EmptyAfterTrim(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	users.putUser(42, "Alex")
	inter, chatRepo, bcast := newTestInteractor(t, rooms, users)

	_, err := inter.Send(context.Background(), "lobby", 42, "   \t  ")
	if !errors.Is(err, ErrEmptyContent) {
		t.Fatalf("Send: err = %v, want ErrEmptyContent", err)
	}
	if chatRepo.createCalls != 0 {
		t.Fatalf("Send: persist must NOT happen on empty-after-trim")
	}
	if len(bcast.calls) != 0 {
		t.Fatalf("Send: broadcast must NOT happen on empty-after-trim")
	}
}

func TestSend_TooLong(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	users.putUser(42, "Alex")
	inter, chatRepo, bcast := newTestInteractor(t, rooms, users)

	long := strings.Repeat("a", entity.MaxChatContentLen+1)
	_, err := inter.Send(context.Background(), "lobby", 42, long)
	if !errors.Is(err, ErrContentTooLong) {
		t.Fatalf("Send: err = %v, want ErrContentTooLong", err)
	}
	if chatRepo.createCalls != 0 {
		t.Fatalf("Send: persist must NOT happen on too-long content")
	}
	if len(bcast.calls) != 0 {
		t.Fatalf("Send: broadcast must NOT happen on too-long content")
	}
}

func TestSend_AtMaxLength_Success(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	users.putUser(42, "Alex")
	inter, _, _ := newTestInteractor(t, rooms, users)

	exact := strings.Repeat("a", entity.MaxChatContentLen)
	out, err := inter.Send(context.Background(), "lobby", 42, exact)
	if err != nil {
		t.Fatalf("Send: at-cap content rejected: %v", err)
	}
	if len(out.Message.Content) != entity.MaxChatContentLen {
		t.Fatalf("Send: at-cap content truncated: len=%d", len(out.Message.Content))
	}
}

func TestSend_NonMember(t *testing.T) {
	rooms := newFakeRoomRepo()
	rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	// 42 is NOT a member.
	users := newFakeUserRepo()
	users.putUser(42, "Alex")
	inter, chatRepo, bcast := newTestInteractor(t, rooms, users)

	_, err := inter.Send(context.Background(), "lobby", 42, "hello")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Send: err = %v, want ErrForbidden", err)
	}
	if chatRepo.createCalls != 0 {
		t.Fatalf("Send: persist must NOT happen for non-member")
	}
	if len(bcast.calls) != 0 {
		t.Fatalf("Send: broadcast must NOT happen for non-member")
	}
}

func TestSend_ArchivedRoom(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusArchived)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	users.putUser(42, "Alex")
	inter, chatRepo, bcast := newTestInteractor(t, rooms, users)

	_, err := inter.Send(context.Background(), "lobby", 42, "hello")
	if !errors.Is(err, ErrArchived) {
		t.Fatalf("Send: err = %v, want ErrArchived", err)
	}
	if chatRepo.createCalls != 0 {
		t.Fatalf("Send: persist must NOT happen on archived room")
	}
	if len(bcast.calls) != 0 {
		t.Fatalf("Send: broadcast must NOT happen on archived room")
	}
}

func TestSend_MissingRoom(t *testing.T) {
	rooms := newFakeRoomRepo()
	users := newFakeUserRepo()
	users.putUser(42, "Alex")
	inter, chatRepo, bcast := newTestInteractor(t, rooms, users)

	_, err := inter.Send(context.Background(), "lobby", 42, "hello")
	if !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("Send: err = %v, want ErrRoomNotFound", err)
	}
	if chatRepo.createCalls != 0 {
		t.Fatalf("Send: persist must NOT happen on missing room")
	}
	if len(bcast.calls) != 0 {
		t.Fatalf("Send: broadcast must NOT happen on missing room")
	}
}

func TestSend_InvalidSlug(t *testing.T) {
	rooms := newFakeRoomRepo()
	users := newFakeUserRepo()
	users.putUser(42, "Alex")
	inter, chatRepo, bcast := newTestInteractor(t, rooms, users)

	_, err := inter.Send(context.Background(), "BAD SLUG", 42, "hello")
	if !errors.Is(err, ErrInvalidSlug) {
		t.Fatalf("Send: err = %v, want ErrInvalidSlug", err)
	}
	if chatRepo.createCalls != 0 {
		t.Fatalf("Send: persist must NOT happen on invalid slug")
	}
	if len(bcast.calls) != 0 {
		t.Fatalf("Send: broadcast must NOT happen on invalid slug")
	}
}

func TestSend_ZeroActor(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 0, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	inter, _, _ := newTestInteractor(t, rooms, users)

	_, err := inter.Send(context.Background(), "lobby", 0, "hello")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Send: err = %v, want ErrForbidden", err)
	}
}

func TestSend_NoBroadcaster_NoPanic(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	users.putUser(42, "Alex")

	inter := NewInteractor(newFakeChatRepo(), rooms, users)
	// SetBroadcaster intentionally NOT called.
	out, err := inter.Send(context.Background(), "lobby", 42, "hello")
	if err != nil {
		t.Fatalf("Send without broadcaster should still succeed: %v", err)
	}
	if out == nil || out.Message == nil {
		t.Fatalf("Send: nil message")
	}
}

func TestListRecent_OldestToNewest_Bounded(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	users := newFakeUserRepo()
	users.putUser(42, "Alex")

	inter, chatRepo, _ := newTestInteractor(t, rooms, users)

	// Seed in arrival order: oldest first, then newer.
	now := time.Now()
	chatRepo.seeded(room.ID,
		entity.RoomChatMessage{ID: 1, RoomID: room.ID, SenderID: 42, Content: "first", CreatedAt: now.Add(-3 * time.Hour)},
		entity.RoomChatMessage{ID: 2, RoomID: room.ID, SenderID: 42, Content: "second", CreatedAt: now.Add(-2 * time.Hour)},
		entity.RoomChatMessage{ID: 3, RoomID: room.ID, SenderID: 42, Content: "third", CreatedAt: now.Add(-1 * time.Hour)},
	)

	// limit 2 → should return the two most recent (second + third), oldest → newest.
	out, err := inter.ListRecent(context.Background(), "lobby", 42, 2)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if got, want := len(out), 2; got != want {
		t.Fatalf("ListRecent: len=%d want %d", got, want)
	}
	if got, want := out[0].Message.Content, "second"; got != want {
		t.Fatalf("ListRecent: out[0].Content=%q want %q (oldest-to-newest)", got, want)
	}
	if got, want := out[1].Message.Content, "third"; got != want {
		t.Fatalf("ListRecent: out[1].Content=%q want %q (oldest-to-newest)", got, want)
	}
}

func TestListRecent_LimitBounds(t *testing.T) {
	rooms := newFakeRoomRepo()
	rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	users := newFakeUserRepo()
	inter, _, _ := newTestInteractor(t, rooms, users)

	for _, lim := range []int{0, -1, MaxChatHistoryLimit + 1} {
		_, err := inter.ListRecent(context.Background(), "lobby", 42, lim)
		if !errors.Is(err, ErrInvalidLimit) {
			t.Fatalf("ListRecent(limit=%d): err=%v want ErrInvalidLimit", lim, err)
		}
	}
}

func TestListRecent_Archived(t *testing.T) {
	rooms := newFakeRoomRepo()
	rooms.putRoom("lobby", "Lobby", entity.RoomStatusArchived)
	users := newFakeUserRepo()
	inter, _, _ := newTestInteractor(t, rooms, users)

	_, err := inter.ListRecent(context.Background(), "lobby", 42, 10)
	if !errors.Is(err, ErrArchived) {
		t.Fatalf("ListRecent on archived: err=%v want ErrArchived", err)
	}
}

func TestListRecent_DisplayNameFallback(t *testing.T) {
	rooms := newFakeRoomRepo()
	room := rooms.putRoom("lobby", "Lobby", entity.RoomStatusActive)
	rooms.putMember(room.ID, 42, entity.RoomRoleGuest)
	// User with empty display_name → fallback.
	users := newFakeUserRepo()
	users.putUser(42, "")

	inter, chatRepo, _ := newTestInteractor(t, rooms, users)
	chatRepo.seeded(room.ID, entity.RoomChatMessage{ID: 1, RoomID: room.ID, SenderID: 42, Content: "x", CreatedAt: time.Now()})

	out, err := inter.ListRecent(context.Background(), "lobby", 42, 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if got, want := out[0].DisplayName, "user #42"; got != want {
		t.Fatalf("DisplayName fallback: got %q want %q", got, want)
	}
}