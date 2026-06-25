package entity

import (
	"errors"
	"regexp"
	"time"
)

// RoomStatus represents the lifecycle state of a room.
type RoomStatus string

const (
	RoomStatusActive   RoomStatus = "active"
	RoomStatusArchived RoomStatus = "archived"
)

// IsValid reports whether s is a recognized RoomStatus.
func (s RoomStatus) IsValid() bool {
	return s == RoomStatusActive || s == RoomStatusArchived
}

// RoomMemberRole represents a user's role inside a single room.
type RoomMemberRole string

const (
	RoomRoleHost  RoomMemberRole = "host"
	RoomRoleAdmin RoomMemberRole = "admin"
	RoomRoleGuest RoomMemberRole = "guest"
)

// IsValid reports whether r is a recognized RoomMemberRole.
func (r RoomMemberRole) IsValid() bool {
	return r == RoomRoleHost || r == RoomRoleAdmin || r == RoomRoleGuest
}

// SlugPattern is the documented URL-safe slug regex (ADR 001 §9).
var SlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// ReservedSlugs cannot be used as room slugs because they collide with
// documented HTTP route families.
var ReservedSlugs = map[string]struct{}{
	"api":    {},
	"admin":  {},
	"static": {},
	"ws":     {},
}

// IsValidSlug reports whether slug matches SlugPattern.
func IsValidSlug(slug string) bool {
	return SlugPattern.MatchString(slug)
}

// IsReservedSlug reports whether slug collides with a reserved route family.
func IsReservedSlug(slug string) bool {
	_, ok := ReservedSlugs[slug]
	return ok
}

// Room represents a single room (the R04 room domain).
type Room struct {
	ID        int64      `json:"id"`
	Slug      string     `json:"slug"`
	Name      string     `json:"name"`
	Status    RoomStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// RoomMember represents a (room, user, role) membership row.
type RoomMember struct {
	RoomID   int64          `json:"room_id"`
	UserID   int            `json:"user_id"`
	Role     RoomMemberRole `json:"role"`
	JoinedAt time.Time      `json:"joined_at"`
}

// RoomInvite represents an invite row. TokenHash is the SHA-256 of the
// plaintext token, base64url-encoded. The plaintext is only returned from
// CreateInvite; everything else sees TokenHash.
type RoomInvite struct {
	ID         int64      `json:"id"`
	RoomID     int64      `json:"room_id"`
	TokenHash  string     `json:"-"` // never serialized; clients hold plaintext
	CreatedBy  int        `json:"created_by_user_id"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	MaxUses    int        `json:"max_uses"`     // 0 == unlimited
	UseCount   int        `json:"use_count"`
}

// ErrInvalidRoomSlug is returned when a slug fails validation.
var ErrInvalidRoomSlug = errors.New("invalid room slug")

// ErrReservedRoomSlug is returned when a slug collides with a reserved route family.
var ErrReservedRoomSlug = errors.New("reserved room slug")