package entity

import "errors"

// ErrSongAlreadyInQueue is returned when attempting to add a song that already exists in the queue.
var ErrSongAlreadyInQueue = errors.New("song already in queue")

// Song represents a YouTube video in the queue.
type Song struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	Duration      int    `json:"duration"` // Duration in seconds
	Thumbnail     string `json:"thumbnail"`
	URL           string `json:"url"`
	AddedBy       string `json:"added_by"`        // Display name of the user who added it
	AddedByID     int    `json:"added_by_id"`     // User ID for ownership check
	IsPrioritized bool   `json:"is_prioritized"`  // Visual indicator for prioritized songs
}

// IsValid checks if the song has the minimum required metadata.
func (s *Song) IsValid() bool {
	return s.ID != "" && s.Title != "" && s.URL != ""
}
