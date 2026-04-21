package entity

// Role represents the permissions of a user.
type Role string

const (
	RoleHost  Role = "host"
	RoleGuest Role = "guest"
	RoleAdmin Role = "admin"
)

// User represents an ephemeral participant in the app.
type User struct {
	DisplayName string `json:"display_name"`
	Role        Role   `json:"role"`
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
