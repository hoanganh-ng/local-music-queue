package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation matching the given constraint name. Mirrors the helper in
// usecase/room but lives here to keep the persistence package self-contained.
func isUniqueViolation(err error, constraintName string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if !strings.Contains(msg, "23505") && !strings.Contains(msg, "unique constraint") && !strings.Contains(msg, "duplicate key") {
		return false
	}
	if constraintName == "" {
		return true
	}
	return strings.Contains(msg, constraintName)
}

// PostgresPlayerLeaseRepository implements repository.PlayerLeaseRepository
// against PostgreSQL. Schema: see 0005_player_leases.up.sql.
type PostgresPlayerLeaseRepository struct {
	db *sql.DB
}

// NewPostgresPlayerLeaseRepository wraps an existing *sql.DB. The DB MUST
// be migrated to schema version 5.
func NewPostgresPlayerLeaseRepository(db *sql.DB) *PostgresPlayerLeaseRepository {
	return &PostgresPlayerLeaseRepository{db: db}
}

// Claim inserts a lease and ends any prior ended-not-yet-archived lease in
// a single transaction. Returns ErrPlayerLeaseExists when an active lease
// is already present (the partial unique index will reject the INSERT).
func (r *PostgresPlayerLeaseRepository) Claim(ctx context.Context, roomID int64, userID int, now time.Time, leaseDuration time.Duration) (*entity.PlayerLease, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	expires := now.Add(leaseDuration)
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO player_leases (room_id, claimed_by_user_id, claimed_at, last_heartbeat_at, expires_at)
		 VALUES ($1, $2, $3, $3, $4)
		 RETURNING id`,
		roomID, userID, now, expires,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err, "idx_player_leases_one_active_per_room") {
			return nil, repository.ErrPlayerLeaseExists
		}
		return nil, fmt.Errorf("insert lease: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return r.GetByRoom(ctx, roomID)
}

// HeartbeatByHolder renews the lease when called by the current holder.
// Returns sql.ErrNoRows when the lease does not exist; the not-holder
// condition returns sql.ErrNoRows (the interactor layer translates).
func (r *PostgresPlayerLeaseRepository) HeartbeatByHolder(ctx context.Context, roomID int64, userID int, now time.Time, leaseDuration time.Duration) (*entity.PlayerLease, error) {
	expires := now.Add(leaseDuration)
	res, err := r.db.ExecContext(ctx,
		`UPDATE player_leases
		 SET last_heartbeat_at = $1, expires_at = $2
		 WHERE room_id = $3 AND ended_at IS NULL AND claimed_by_user_id = $4`,
		now, expires, roomID, userID)
	if err != nil {
		return nil, fmt.Errorf("heartbeat: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetByRoom(ctx, roomID)
}

// ReleaseByHolder ends the lease and returns whether a row was updated.
func (r *PostgresPlayerLeaseRepository) ReleaseByHolder(ctx context.Context, roomID int64, userID int, now time.Time) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE player_leases SET ended_at = $1
		 WHERE room_id = $2 AND ended_at IS NULL AND claimed_by_user_id = $3`,
		now, roomID, userID)
	if err != nil {
		return false, fmt.Errorf("release: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}

// EndLease ends the active lease for a room (used by sweep + explicit
// archive). Returns sql.ErrNoRows when no active lease exists.
func (r *PostgresPlayerLeaseRepository) EndLease(ctx context.Context, roomID int64, now time.Time) (*entity.PlayerLease, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE player_leases SET ended_at = $1
		 WHERE room_id = $2 AND ended_at IS NULL`,
		now, roomID)
	if err != nil {
		return nil, fmt.Errorf("end lease: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	return r.GetByRoom(ctx, roomID)
}

// GetByRoom fetches the active lease for a room.
func (r *PostgresPlayerLeaseRepository) GetByRoom(ctx context.Context, roomID int64) (*entity.PlayerLease, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, room_id, claimed_by_user_id, claimed_at, last_heartbeat_at, expires_at, ended_at
		 FROM player_leases WHERE room_id = $1 AND ended_at IS NULL`,
		roomID)
	return scanLeaseRow(row)
}

// ListActive returns active (not ended) leases ordered by expires_at ASC.
func (r *PostgresPlayerLeaseRepository) ListActive(ctx context.Context, now time.Time) ([]entity.PlayerLease, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, room_id, claimed_by_user_id, claimed_at, last_heartbeat_at, expires_at, ended_at
		 FROM player_leases WHERE ended_at IS NULL ORDER BY expires_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list active: %w", err)
	}
	defer rows.Close()
	var out []entity.PlayerLease
	for rows.Next() {
		l, err := scanLeaseRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func scanLeaseRow(s interface {
	Scan(dest ...interface{}) error
}) (*entity.PlayerLease, error) {
	var l entity.PlayerLease
	var endedAt sql.NullTime
	if err := s.Scan(&l.ID, &l.RoomID, &l.ClaimedByUserID, &l.ClaimedAt, &l.LastHeartbeatAt, &l.ExpiresAt, &endedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if endedAt.Valid {
		t := endedAt.Time
		l.EndedAt = &t
	}
	return &l, nil
}