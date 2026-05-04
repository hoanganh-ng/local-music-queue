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

// ContainsSong checks if a song with the given video ID is already in the queue.
func (q *Queue) ContainsSong(videoID string) bool {
	for _, song := range q.Songs {
		if song.ID == videoID {
			return true
		}
	}
	return false
}

// Add adds a song to the end of the queue.
func (q *Queue) Add(song Song) {
	q.Songs = append(q.Songs, song)
	if q.CurrentIndex == -1 {
		q.CurrentIndex = 0
		q.Status = StatusPlaying
	}
}

// Remove removes a song at the specified index from the queue.
func (q *Queue) Remove(index int) error {
	if index < 0 || index >= len(q.Songs) {
		return errors.New("invalid index")
	}

	// Remove the song
	q.Songs = append(q.Songs[:index], q.Songs[index+1:]...)

	// Adjust the current index if necessary
	if index < q.CurrentIndex {
		q.CurrentIndex--
	} else if index == q.CurrentIndex {
		// If the currently playing song is removed
		if len(q.Songs) == 0 {
			q.CurrentIndex = -1
			q.Status = StatusIdle
			q.Elapsed = 0
		} else if q.CurrentIndex >= len(q.Songs) {
			// It was the last song in the list
			q.CurrentIndex = len(q.Songs) - 1
			q.Status = StatusPlaying
			q.Elapsed = 0
		} else {
			// A new song takes its place
			q.Elapsed = 0
			q.Status = StatusPlaying
		}
	}

	return nil
}

// Clear removes all upcoming songs from the queue, leaving only the currently playing one.
func (q *Queue) Clear() {
	if q.CurrentIndex == -1 || len(q.Songs) == 0 {
		q.Songs = []Song{}
		q.CurrentIndex = -1
		q.Status = StatusIdle
		q.Elapsed = 0
		return
	}

	// Keep only the currently playing song
	currentSong := q.Songs[q.CurrentIndex]
	q.Songs = []Song{currentSong}
	q.CurrentIndex = 0
}

// IsValidTransition checks if a status transition is valid (simplified).
func (q *Queue) IsValidTransition(newStatus PlaybackStatus) bool {
	if q.CurrentIndex == -1 && newStatus != StatusIdle {
		return false
	}
	return true
}

// Prioritize moves a song to the front of the queue (after current song).
func (q *Queue) Prioritize(songIndex int) error {
	// Validate index
	if songIndex < 0 || songIndex >= len(q.Songs) {
		return errors.New("invalid song index")
	}

	// Cannot prioritize currently playing song
	if songIndex == q.CurrentIndex {
		return errors.New("cannot prioritize currently playing song")
	}

	// Target position: right after current song
	targetIndex := q.CurrentIndex + 1

	// Already at target position
	if songIndex == targetIndex {
		return nil
	}

	// Extract and mark song
	song := q.Songs[songIndex]
	song.IsPrioritized = true

	// Remove from current position
	q.Songs = append(q.Songs[:songIndex], q.Songs[songIndex+1:]...)

	// Adjust target if removed from before it
	if songIndex < targetIndex {
		targetIndex--
	}

	// Insert at target position
	q.Songs = append(q.Songs[:targetIndex], append([]Song{song}, q.Songs[targetIndex:]...)...)

	// Adjust current index if needed
	if songIndex < q.CurrentIndex {
		q.CurrentIndex--
	}
	if targetIndex <= q.CurrentIndex {
		q.CurrentIndex++
	}

	return nil
}
