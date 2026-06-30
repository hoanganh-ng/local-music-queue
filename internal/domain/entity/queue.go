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
		q.Status = StatusPaused
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

// ContainsSong checks if a song with the given video ID is already in the upcoming queue.
// It only checks songs that haven't been played yet (after CurrentIndex).
func (q *Queue) ContainsSong(videoID string) bool {
	// Start checking from the next song after current (CurrentIndex + 1)
	startIndex := q.CurrentIndex + 1
	if startIndex < 0 {
		startIndex = 0
	}

	for i := startIndex; i < len(q.Songs); i++ {
		if q.Songs[i].ID == videoID {
			return true
		}
	}
	return false
}

// Add adds a song to the end of the queue.
func (q *Queue) Add(song Song) {
	oldLength := len(q.Songs)
	q.Songs = append(q.Songs, song)

	// Auto-start playback if:
	// 1. Queue was completely empty (CurrentIndex == -1), OR
	// 2. Queue was at the last song and is now paused (finished playing)
	if q.CurrentIndex == -1 {
		q.CurrentIndex = 0
		q.Status = StatusPlaying
		q.Elapsed = 0
	} else if q.CurrentIndex == oldLength-1 && q.Status == StatusPaused {
		// If we were at the last song and paused (song ended), move to the new song
		q.CurrentIndex = oldLength
		q.Status = StatusPlaying
		q.Elapsed = 0
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

// ErrNoCurrentSong is returned by the room playback helpers when a
// mutation needs a valid current song and the queue does not have one
// (CurrentIndex == -1 or no songs). Mirrors the global queue's
// ErrQueueEmpty so the delivery layer can map it consistently.
var ErrNoCurrentSong = errors.New("no current song")

// ErrInvalidStatus is returned by SetStatus when the supplied
// PlaybackStatus is not one of the recognized values. Client-driven
// "idle" is rejected so the API surface cannot transition the queue
// into the idle state; only natural end-of-queue / clear paths can.
var ErrInvalidStatus = errors.New("invalid playback status")

// ErrInvalidElapsed is returned by SetElapsed when the supplied
// elapsed value is negative. The wire shape distinguishes missing
// from 0; negative is always invalid.
var ErrInvalidElapsed = errors.New("invalid elapsed value")

// SetStatus mutates the queue to newStatus. It refuses to mutate when
// the queue has no current song (CurrentIndex == -1 or empty Songs),
// returning ErrNoCurrentSong without touching state. Refuses the
// idle status outright (clients cannot request it). When transitioning
// to playing from a previously paused state the elapsed counter is
// left untouched; when transitioning to paused, elapsed is left
// untouched as well. The caller is responsible for serializing the
// mutation under the queue's lock.
func (q *Queue) SetStatus(newStatus PlaybackStatus) error {
	if newStatus != StatusPlaying && newStatus != StatusPaused {
		return ErrInvalidStatus
	}
	if q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Songs) {
		return ErrNoCurrentSong
	}
	q.Status = newStatus
	return nil
}

// SetElapsed clamps a non-negative elapsed value into the queue. It
// refuses negative values (ErrInvalidElapsed) and refuses to mutate
// when the queue has no current song (ErrNoCurrentSong).
func (q *Queue) SetElapsed(elapsed int) error {
	if elapsed < 0 {
		return ErrInvalidElapsed
	}
	if q.CurrentIndex < 0 || q.CurrentIndex >= len(q.Songs) {
		return ErrNoCurrentSong
	}
	q.Elapsed = elapsed
	return nil
}

// AdvanceToNext moves the current index to the next song only when a
// next song exists, resetting elapsed to 0 and forcing status to
// playing. It deliberately does NOT mutate state when there is no
// next song (the global queue.Next sets Status = StatusPaused in
// that branch, which would leave the queue partially mutated on a
// no-next skip; R09a avoids that by checking before mutating).
//
// Returns the previous index, the new (advanced) song pointer (nil
// when no advance), and a sentinel:
//   - ErrNoNextSong      → no next song, queue not mutated
//   - ErrNoCurrentSong   → empty queue, queue not mutated
//   - nil                → advance succeeded
func (q *Queue) AdvanceToNext() (prevIndex int, newSong *Song, err error) {
	if len(q.Songs) == 0 || q.CurrentIndex < 0 {
		return q.CurrentIndex, nil, ErrNoCurrentSong
	}
	if q.CurrentIndex >= len(q.Songs)-1 {
		return q.CurrentIndex, nil, ErrNoNextSong
	}
	prevIndex = q.CurrentIndex
	q.CurrentIndex++
	q.Elapsed = 0
	q.Status = StatusPlaying
	cs := q.Songs[q.CurrentIndex]
	return prevIndex, &cs, nil
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
