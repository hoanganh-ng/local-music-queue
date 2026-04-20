package entity

// Role represents the permissions of a user.
type Role string

const (
	RoleHost  Role = "host"
	RoleGuest Role = "guest"
)

// User represents an ephemeral participant in the app.
type User struct {
	DisplayName string `json:"display_name"`
	Role        Role   `json:"role"`
}

// CanControlPlayback checks if the user has permission to change playback state.
func (u *User) CanControlPlayback() bool {
	return u.Role == RoleHost
}
