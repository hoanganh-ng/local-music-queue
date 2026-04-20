package entity

import "time"

// ActivityType represents the kind of action that occurred.
type ActivityType string

const (
	ActivitySongAdded   ActivityType = "song_added"
	ActivitySongSkipped ActivityType = "song_skipped"
	ActivityPlayback    ActivityType = "playback_changed"
	ActivityUserJoined  ActivityType = "user_joined"
)

// Activity represents a log entry in the activity feed.
type Activity struct {
	Timestamp   time.Time    `json:"timestamp"`
	Type        ActivityType `json:"type"`
	User        string       `json:"user"`    // Display name of the user
	Description string       `json:"description"`
}

// NewActivity creates a new activity entry.
func NewActivity(activityType ActivityType, user, description string) Activity {
	return Activity{
		Timestamp:   time.Now(),
		Type:        activityType,
		User:        user,
		Description: description,
	}
}
