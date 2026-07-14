package repository

import (
	"context"
	"time"

	"local-music-queue/internal/domain/entity"
)

// RoomChatMessageRepository defines persistence operations for plain-text
// room chat messages (R11a). The implementation is responsible for the
// `idx_room_chat_messages_room_created` index, FK integrity against
// rooms(id) and users(id), and the constant-time reply of a freshly
// created row carrying the server-stamped CreatedAt.
type RoomChatMessageRepository interface {
	// CreateMessage inserts a chat row and returns the persisted entity
	// with its DB-assigned id and CreatedAt stamp.
	CreateMessage(ctx context.Context, roomID int64, senderID int, content string, now time.Time) (*entity.RoomChatMessage, error)

	// ListRecentMessages returns the most recent chat messages for a
	// room, ordered oldest → newest (the caller renders them top-to-
	// bottom). Limit must be > 0 and <= the per-call cap (the handler
	// enforces a 1..100 bound).
	ListRecentMessages(ctx context.Context, roomID int64, limit int) ([]entity.RoomChatMessage, error)
}