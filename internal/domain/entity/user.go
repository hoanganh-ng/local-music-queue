package entity

import "time"

// Role represents the permissions of a user.
type Role string

const (
	RoleHost  Role = "host"
	RoleGuest Role = "guest"
	RoleAdmin Role = "admin"
)

// User represents a participant in the app.
type User struct {
	ID              int       `json:"id"`
	Email           string    `json:"email"`
	DisplayName     string    `json:"display_name"`
	ProfilePicture  string    `json:"profile_picture"`
	Role            Role      `json:"role"`
	PriorityBalance int       `json:"priority_balance"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CanControlPlayback checks if the user has permission to change playback state.
// Both hosts and admins can control playback.
func (u *User) CanControlPlayback() bool {
	return u.Role == RoleHost || u.Role == RoleAdmin
}

// CanHostPlayer checks if the user should render and manage the YouTube iframe player.
// Only the host hosts the player; admins control playback remotely.
func (u *User) CanHostPlayer() bool {
	return u.Role == RoleHost
}

// CanUsePriority checks if the user has priority tokens available.
func (u *User) CanUsePriority() bool {
	return u.PriorityBalance > 0
}
