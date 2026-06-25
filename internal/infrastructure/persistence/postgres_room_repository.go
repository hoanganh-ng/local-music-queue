package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// PostgresRoomRepository implements repository.RoomRepository on PostgreSQL.
// Schema: see 0004_rooms.up.sql.
type PostgresRoomRepository struct {
	db *sql.DB
}

// NewPostgresRoomRepository constructs a PostgresRoomRepository over an
// existing *sql.DB. The DB MUST already be migrated to schema version 4.
func NewPostgresRoomRepository(db *sql.DB) *PostgresRoomRepository {
	return &PostgresRoomRepository{db: db}
}

// CreateRoomAndHost inserts the room row and the creator's host membership
// in a single transaction. The exactly-one-host invariant is enforced by the
// partial unique index `idx_room_members_one_host_per_room`; the INSERT
// order (room first, member second) ensures the FK is satisfied.
func (r *PostgresRoomRepository) CreateRoomAndHost(ctx context.Context, slug, name string, creatorUserID int, now time.Time) (*entity.Room, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var roomID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		 VALUES ($1, $2, 'active', $3, $3)
		 RETURNING id`,
		slug, name, now,
	).Scan(&roomID)
	if err != nil {
		return nil, fmt.Errorf("insert room: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO room_members (room_id, user_id, role, joined_at)
		 VALUES ($1, $2, 'host', $3)`,
		roomID, creatorUserID, now,
	); err != nil {
		return nil, fmt.Errorf("insert host member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return r.GetRoomByID(ctx, roomID)
}

// CreateRoom inserts only the room row. Use CreateRoomAndHost in production
// so the host membership is created atomically; this method exists for tests.
func (r *PostgresRoomRepository) CreateRoom(ctx context.Context, slug, name string, now time.Time) (*entity.Room, error) {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO rooms (slug, name, status, created_at, updated_at)
		 VALUES ($1, $2, 'active', $3, $3)
		 RETURNING id`,
		slug, name, now,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("insert room: %w", err)
	}
	return r.GetRoomByID(ctx, id)
}

// GetRoomByID fetches a room by its primary key.
func (r *PostgresRoomRepository) GetRoomByID(ctx context.Context, id int64) (*entity.Room, error) {
	return r.scanOne(ctx,
		`SELECT id, slug, name, status, created_at, updated_at FROM rooms WHERE id = $1`, id)
}

// GetRoomBySlug fetches a room by its slug.
func (r *PostgresRoomRepository) GetRoomBySlug(ctx context.Context, slug string) (*entity.Room, error) {
	return r.scanOne(ctx,
		`SELECT id, slug, name, status, created_at, updated_at FROM rooms WHERE slug = $1`, slug)
}

// ListRooms returns rooms filtered by status. status=="" returns every room.
func (r *PostgresRoomRepository) ListRooms(ctx context.Context, status entity.RoomStatus) ([]entity.Room, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if status == "" {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, slug, name, status, created_at, updated_at FROM rooms ORDER BY id ASC`)
	} else {
		rows, err = r.db.QueryContext(ctx,
			`SELECT id, slug, name, status, created_at, updated_at FROM rooms WHERE status = $1 ORDER BY id ASC`, status)
	}
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()
	var out []entity.Room
	for rows.Next() {
		room, err := scanRoomRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *room)
	}
	return out, rows.Err()
}

// ArchiveRoom transitions a room to 'archived'. Used by internal-only
// archive method (Task 9) and tests; no public archive endpoint in R04.
func (r *PostgresRoomRepository) ArchiveRoom(ctx context.Context, roomID int64, now time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE rooms SET status = 'archived', updated_at = $1 WHERE id = $2`, now, roomID)
	if err != nil {
		return fmt.Errorf("archive room: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddMember inserts a membership row. Caller is responsible for invariant
// checks (one host per room).
func (r *PostgresRoomRepository) AddMember(ctx context.Context, roomID int64, userID int, role entity.RoomMemberRole, now time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO room_members (room_id, user_id, role, joined_at)
		 VALUES ($1, $2, $3, $4)`,
		roomID, userID, role, now,
	)
	if err != nil {
		return fmt.Errorf("insert member: %w", err)
	}
	return nil
}

// GetMember fetches a (room, user) membership row.
func (r *PostgresRoomRepository) GetMember(ctx context.Context, roomID int64, userID int) (*entity.RoomMember, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT room_id, user_id, role, joined_at FROM room_members WHERE room_id = $1 AND user_id = $2`,
		roomID, userID)
	var m entity.RoomMember
	var roomIDOut int64
	var role string
	if err := row.Scan(&roomIDOut, &m.UserID, &role, &m.JoinedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("scan member: %w", err)
	}
	m.RoomID = roomIDOut
	m.Role = entity.RoomMemberRole(role)
	return &m, nil
}

// ListMembers returns every member of a room.
func (r *PostgresRoomRepository) ListMembers(ctx context.Context, roomID int64) ([]entity.RoomMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT room_id, user_id, role, joined_at FROM room_members WHERE room_id = $1 ORDER BY joined_at ASC`, roomID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()
	var out []entity.RoomMember
	for rows.Next() {
		var m entity.RoomMember
		var roomIDOut int64
		var role string
		if err := rows.Scan(&roomIDOut, &m.UserID, &role, &m.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}
		m.RoomID = roomIDOut
		m.Role = entity.RoomMemberRole(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateMemberRole updates a member's role. Caller must enforce invariants.
func (r *PostgresRoomRepository) UpdateMemberRole(ctx context.Context, roomID int64, userID int, role entity.RoomMemberRole, now time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE room_members SET role = $1 WHERE room_id = $2 AND user_id = $3`,
		role, roomID, userID)
	if err != nil {
		return fmt.Errorf("update member role: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CountHosts returns the number of host memberships in a room. Used to
// enforce the exactly-one-host invariant in the use-case layer.
func (r *PostgresRoomRepository) CountHosts(ctx context.Context, roomID int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM room_members WHERE room_id = $1 AND role = 'host'`, roomID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count hosts: %w", err)
	}
	return n, nil
}

// CreateInvite inserts an invite row.
func (r *PostgresRoomRepository) CreateInvite(ctx context.Context, invite *entity.RoomInvite) error {
	var id int64
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO room_invites (room_id, token_hash, created_by, created_at, expires_at, revoked_at, max_uses, use_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		invite.RoomID, invite.TokenHash, invite.CreatedBy, invite.CreatedAt, invite.ExpiresAt,
		invite.RevokedAt, invite.MaxUses, invite.UseCount,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("insert invite: %w", err)
	}
	invite.ID = id
	return nil
}

// GetInviteByID fetches an invite by (room_id, invite_id).
func (r *PostgresRoomRepository) GetInviteByID(ctx context.Context, roomID int64, inviteID int64) (*entity.RoomInvite, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, room_id, token_hash, created_by, created_at, expires_at, revoked_at, max_uses, use_count
		 FROM room_invites WHERE room_id = $1 AND id = $2`,
		roomID, inviteID)
	return scanInviteRow(row)
}

// GetInviteByTokenHash fetches an invite by its stored token hash.
func (r *PostgresRoomRepository) GetInviteByTokenHash(ctx context.Context, tokenHash string) (*entity.RoomInvite, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, room_id, token_hash, created_by, created_at, expires_at, revoked_at, max_uses, use_count
		 FROM room_invites WHERE token_hash = $1`,
		tokenHash)
	return scanInviteRow(row)
}

// ListInvites lists invites for a room, newest first.
func (r *PostgresRoomRepository) ListInvites(ctx context.Context, roomID int64) ([]entity.RoomInvite, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, room_id, token_hash, created_by, created_at, expires_at, revoked_at, max_uses, use_count
		 FROM room_invites WHERE room_id = $1 ORDER BY created_at DESC`, roomID)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()
	var out []entity.RoomInvite
	for rows.Next() {
		inv, err := scanInviteRowRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// RevokeInvite marks an invite as revoked. Returns sql.ErrNoRows when the
// invite does not belong to the room or has already been revoked.
func (r *PostgresRoomRepository) RevokeInvite(ctx context.Context, roomID int64, inviteID int64, now time.Time) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE room_invites SET revoked_at = $1 WHERE room_id = $2 AND id = $3 AND revoked_at IS NULL`,
		now, roomID, inviteID)
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// IncrementInviteUseCount atomically bumps use_count and returns the new value.
func (r *PostgresRoomRepository) IncrementInviteUseCount(ctx context.Context, inviteID int64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE room_invites SET use_count = use_count + 1 WHERE id = $1`, inviteID)
	if err != nil {
		return fmt.Errorf("increment use count: %w", err)
	}
	return nil
}

// RedeemInviteAtomic performs the member-insert + use-count-increment
// inside a single transaction. The conditional UPDATE on room_invites uses
// `(max_uses = 0 OR use_count < max_uses)` so the row is only bumped when
// the invite still has capacity; if it is already at the limit, RowsAffected
// is 0 and we surface ErrInviteExhausted. The transaction is rolled back on
// any error so the member row never persists when the invite cannot be
// consumed.
func (r *PostgresRoomRepository) RedeemInviteAtomic(ctx context.Context, inviteID int64, roomID int64, userID int, role entity.RoomMemberRole, maxUses int, now time.Time) (*entity.RoomMember, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// First, try the conditional increment. maxUses is part of the WHERE so
	// a concurrent redeemer that bumps use_count to max_uses makes this
	// statement affect 0 rows and we surface ErrInviteExhausted.
	res, err := tx.ExecContext(ctx,
		`UPDATE room_invites SET use_count = use_count + 1
		 WHERE id = $1 AND ($2 = 0 OR use_count < $2)`,
		inviteID, maxUses)
	if err != nil {
		return nil, fmt.Errorf("conditional increment: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, repository.ErrInviteExhausted
	}

	// Increment succeeded; insert the member row. If this fails, the
	// transaction rolls back, undoing the use-count bump.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO room_members (room_id, user_id, role, joined_at)
		 VALUES ($1, $2, $3, $4)`,
		roomID, userID, role, now,
	); err != nil {
		return nil, fmt.Errorf("insert member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return r.GetMember(ctx, roomID, userID)
}

// --- helpers ---

func (r *PostgresRoomRepository) scanOne(ctx context.Context, query string, args ...interface{}) (*entity.Room, error) {
	row := r.db.QueryRowContext(ctx, query, args...)
	room, err := scanRoomRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return room, nil
}

// rowScanner is implemented by both *sql.Row and *sql.Rows so the same scan
// helper works for QueryRow and rows.Next() paths.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanRoomRow(s rowScanner) (*entity.Room, error) {
	var room entity.Room
	var id int64
	var status string
	if err := s.Scan(&id, &room.Slug, &room.Name, &status, &room.CreatedAt, &room.UpdatedAt); err != nil {
		return nil, err
	}
	room.ID = id
	room.Status = entity.RoomStatus(status)
	return &room, nil
}

func scanInviteRow(s rowScanner) (*entity.RoomInvite, error) {
	inv, err := scanInviteRowGeneric(s)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	return inv, nil
}

func scanInviteRowRows(s rowScanner) (*entity.RoomInvite, error) {
	return scanInviteRowGeneric(s)
}

func scanInviteRowGeneric(s rowScanner) (*entity.RoomInvite, error) {
	var inv entity.RoomInvite
	var id, roomID, createdBy int64
	var revokedAt sql.NullTime
	if err := s.Scan(&id, &roomID, &inv.TokenHash, &createdBy, &inv.CreatedAt, &inv.ExpiresAt, &revokedAt, &inv.MaxUses, &inv.UseCount); err != nil {
		return nil, err
	}
	inv.ID = id
	inv.RoomID = roomID
	inv.CreatedBy = int(createdBy)
	if revokedAt.Valid {
		t := revokedAt.Time
		inv.RevokedAt = &t
	}
	return &inv, nil
}
