package entity

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel errors
var (
	ErrAlreadyVoted       = errors.New("user has already voted in this session")
	ErrVoteSessionExpired = errors.New("vote session has expired")
	ErrVoteOnCurrentSong  = errors.New("cannot start a priority vote on the currently playing song")
	ErrSongNotFound       = errors.New("song not found at the given index")
)

// VoteType constants
type VoteType string

const (
	VoteTypeSkip       VoteType = "skip"
	VoteTypePrioritize VoteType = "prioritize"
)

// VoteSession represents an active voting session
type VoteSession struct {
	ID        string       `json:"id"`
	Type      VoteType     `json:"type"`
	SongID    string       `json:"song_id"`
	SongTitle string       `json:"song_title"`
	SongIndex int          `json:"song_index"`
	VotedBy   map[int]bool `json:"voted_by"`
	Threshold int          `json:"threshold"`
	CreatedAt time.Time    `json:"created_at"`
	ExpiresAt time.Time    `json:"expires_at"`
}

// ExpiredSession carries data about a session that was just evicted.
type ExpiredSession struct {
	SessionID string
	Activity  Activity
}

// NewVoteSession creates a new vote session
func NewVoteSession(voteType VoteType, song Song, songIndex, threshold int, expiry time.Duration) *VoteSession {
	now := time.Now()
	return &VoteSession{
		ID:        fmt.Sprintf("%s:%s", voteType, song.ID),
		Type:      voteType,
		SongID:    song.ID,
		SongTitle: song.Title,
		SongIndex: songIndex,
		VotedBy:   make(map[int]bool),
		Threshold: threshold,
		CreatedAt: now,
		ExpiresAt: now.Add(expiry),
	}
}

// Cast records a vote from a user
func (v *VoteSession) Cast(userID int) error {
	if v.IsExpired() {
		return ErrVoteSessionExpired
	}
	if v.VotedBy[userID] {
		return ErrAlreadyVoted
	}
	v.VotedBy[userID] = true
	return nil
}

// HasVoted checks if a user has already voted
func (v *VoteSession) HasVoted(userID int) bool {
	return v.VotedBy[userID]
}

// VoteCount returns the number of votes cast
func (v *VoteSession) VoteCount() int {
	return len(v.VotedBy)
}

// IsPassed checks if the vote has reached the threshold
func (v *VoteSession) IsPassed() bool {
	return v.VoteCount() >= v.Threshold
}

// IsExpired checks if the vote session has expired
func (v *VoteSession) IsExpired() bool {
	return time.Now().After(v.ExpiresAt)
}

// IsExpiredAt reports whether the session expired at the supplied
// time. Used by packages that drive their own clock (the roomvote
// interactor). Equivalent semantics to IsExpired() but parameterised.
func (v *VoteSession) IsExpiredAt(t time.Time) bool {
	return !t.Before(v.ExpiresAt)
}

// RemainingSeconds returns the number of seconds until expiry
func (v *VoteSession) RemainingSeconds() int {
	remaining := int(time.Until(v.ExpiresAt).Seconds())
	if remaining < 0 {
		return 0
	}
	return remaining
}

// MajorityThreshold calculates the votes needed for a strict majority
func MajorityThreshold(connectedUsers int) int {
	threshold := connectedUsers / 2
	if threshold < 2 {
		return 2
	}
	return threshold
}
