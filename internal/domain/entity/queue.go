package entity

import "errors"

var (
	ErrQueueEmpty = errors.New("queue is empty")
	ErrNoNextSong = errors.New("no next song in queue")
)

// PlaybackStatus represents the current state of playback.
type PlaybackStatus string

const (
	StatusPlaying PlaybackStatus = "playing"
	StatusPaused  PlaybackStatus = "paused"
	StatusIdle    PlaybackStatus = "idle"
)

// Queue represents the shared music queue.
type Queue struct {
	Songs        []Song         `json:"songs"`
	CurrentIndex int            `json:"current_index"`
	Status       PlaybackStatus `json:"status"`
	Elapsed      int            `json:"elapsed"` // Elapsed time in seconds
	History      []Activity     `json:"history"`
}

// NewQueue creates a new empty queue.
func NewQueue() *Queue {
	return &Queue{
		Songs:        []Song{},
		CurrentIndex: -1,
		Status:       StatusIdle,
		Elapsed:      0,
		History:      []Activity{},
	}
}

// CurrentSong returns the song currently being played.
func (q *Queue) CurrentSong() (*Song, error) {
	if len(q.Songs) == 0 {
		return nil, ErrQueueEmpty
	}
	if q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Songs) {
		return nil, errors.New("invalid current index")
	}
	return &q.Songs[q.CurrentIndex], nil
}

// Next advances the queue to the next song.
func (q *Queue) Next() error {
	if len(q.Songs) == 0 {
		return ErrQueueEmpty
	}
	if q.CurrentIndex >= len(q.Songs)-1 {
		return ErrNoNextSong
	}
	q.CurrentIndex++
	q.Elapsed = 0
	q.Status = StatusPlaying
	return nil
}

// Prev moves the queue to the previous song.
func (q *Queue) Prev() error {
	if len(q.Songs) == 0 {
		return ErrQueueEmpty
	}
	if q.CurrentIndex <= 0 {
		return errors.New("already at the first song")
	}
	q.CurrentIndex--
	q.Elapsed = 0
	q.Status = StatusPlaying
	return nil
}

// Add adds a song to the end of the queue.
func (q *Queue) Add(song Song) {
	q.Songs = append(q.Songs, song)
	if q.CurrentIndex == -1 {
		q.CurrentIndex = 0
		q.Status = StatusPlaying
	}
}

// IsValidTransition checks if a status transition is valid (simplified).
func (q *Queue) IsValidTransition(newStatus PlaybackStatus) bool {
	if q.CurrentIndex == -1 && newStatus != StatusIdle {
		return false
	}
	return true
}
