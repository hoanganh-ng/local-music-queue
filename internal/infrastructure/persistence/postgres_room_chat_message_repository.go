package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"local-music-queue/internal/domain/entity"
)

// PostgresRoomChatMessageRepository implements
// repository.RoomChatMessageRepository against PostgreSQL. Schema:
// see 0008_room_chat_messages.up.sql.
//
// The implementation is safe for concurrent use — every operation is
// a single-row insert or a bounded SELECT against the
// `idx_room_chat_messages_room_created` index. No coordinator mutex
// is needed at this layer.
type PostgresRoomChatMessageRepository struct {
	db *sql.DB
}

// NewPostgresRoomChatMessageRepository wraps an existing *sql.DB. The
// DB MUST be migrated to schema version 8.
func NewPostgresRoomChatMessageRepository(db *sql.DB) *PostgresRoomChatMessageRepository {
	return &PostgresRoomChatMessageRepository{db: db}
}

// CreateMessage inserts a chat row and returns the persisted entity
// with the DB-assigned id and CreatedAt stamp. Content is persisted
// exactly as supplied — the interactor owns trim / CRLF normalization
// / length validation, so the repo does not re-validate.
func (r *PostgresRoomChatMessageRepository) CreateMessage(ctx context.Context, roomID int64, senderID int, content string, now time.Time) (*entity.RoomChatMessage, error) {
	var msg entity.RoomChatMessage
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO room_chat_messages (room_id, sender_id, content, created_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, room_id, sender_id, content, created_at`,
		roomID, senderID, content, now,
	).Scan(&id, &msg.RoomID, &msg.SenderID, &msg.Content, &msg.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert chat message: %w", err)
	}
	msg.ID = id
	return &msg, nil
}

// ListRecentMessages returns up to `limit` most recent chat messages
// for the room, ordered oldest → newest (the caller renders them
// top-to-bottom). Limit is clamped to the handler-enforced 1..100
// range; the repo trusts the caller's bound.
func (r *PostgresRoomChatMessageRepository) ListRecentMessages(ctx context.Context, roomID int64, limit int) ([]entity.RoomChatMessage, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, room_id, sender_id, content, created_at
		 FROM (
		   SELECT id, room_id, sender_id, content, created_at
		   FROM room_chat_messages
		   WHERE room_id = $1
		   ORDER BY created_at DESC, id DESC
		   LIMIT $2
		 ) recent
		 ORDER BY created_at ASC, id ASC`,
		roomID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	defer rows.Close()

	out := make([]entity.RoomChatMessage, 0, limit)
	for rows.Next() {
		var msg entity.RoomChatMessage
		var id int64
		if err := rows.Scan(&id, &msg.RoomID, &msg.SenderID, &msg.Content, &msg.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chat message: %w", err)
		}
		msg.ID = id
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Compile-time interface assertion.
var _ = sql.ErrNoRows