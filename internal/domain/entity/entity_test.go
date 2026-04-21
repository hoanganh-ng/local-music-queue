package entity

import (
	"testing"
	"time"
)

// --- Song Tests ---

func TestSong_IsValid(t *testing.T) {
	tests := []struct {
		name string
		song Song
		want bool
	}{
		{
			name: "valid song",
			song: Song{ID: "abc", Title: "My Song", URL: "https://youtube.com/watch?v=abc"},
			want: true,
		},
		{
			name: "missing ID",
			song: Song{Title: "My Song", URL: "https://youtube.com/watch?v=abc"},
			want: false,
		},
		{
			name: "missing title",
			song: Song{ID: "abc", URL: "https://youtube.com/watch?v=abc"},
			want: false,
		},
		{
			name: "missing URL",
			song: Song{ID: "abc", Title: "My Song"},
			want: false,
		},
		{
			name: "all empty",
			song: Song{},
			want: false,
		},
		{
			name: "optional fields missing is still valid",
			song: Song{ID: "abc", Title: "My Song", URL: "url"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.song.IsValid()
			if got != tt.want {
				t.Errorf("Song.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Queue Tests ---

func TestQueue_NewQueue(t *testing.T) {
	q := NewQueue()

	if len(q.Songs) != 0 {
		t.Errorf("expected empty songs, got %d", len(q.Songs))
	}
	if q.CurrentIndex != -1 {
		t.Errorf("expected current index -1, got %d", q.CurrentIndex)
	}
	if q.Status != StatusIdle {
		t.Errorf("expected status idle, got %s", q.Status)
	}
	if q.Elapsed != 0 {
		t.Errorf("expected elapsed 0, got %d", q.Elapsed)
	}
}

func TestQueue_Add_FirstSong(t *testing.T) {
	q := NewQueue()
	song := Song{ID: "1", Title: "Song 1", URL: "url1"}

	q.Add(song)

	if len(q.Songs) != 1 {
		t.Errorf("expected 1 song, got %d", len(q.Songs))
	}
	if q.CurrentIndex != 0 {
		t.Errorf("expected current index 0, got %d", q.CurrentIndex)
	}
	if q.Status != StatusPlaying {
		t.Errorf("expected status playing, got %s", q.Status)
	}
}

func TestQueue_Add_SubsequentSongs(t *testing.T) {
	q := NewQueue()
	q.Add(Song{ID: "1", Title: "Song 1", URL: "url1"})
	q.Add(Song{ID: "2", Title: "Song 2", URL: "url2"})
	q.Add(Song{ID: "3", Title: "Song 3", URL: "url3"})

	if len(q.Songs) != 3 {
		t.Errorf("expected 3 songs, got %d", len(q.Songs))
	}
	// Adding more songs should NOT change the current index
	if q.CurrentIndex != 0 {
		t.Errorf("expected current index to remain 0, got %d", q.CurrentIndex)
	}
	// Verify order
	if q.Songs[2].ID != "3" {
		t.Errorf("expected third song ID '3', got '%s'", q.Songs[2].ID)
	}
}

func TestQueue_Next_Success(t *testing.T) {
	q := NewQueue()
	q.Add(Song{ID: "1", Title: "Song 1", URL: "url1"})
	q.Add(Song{ID: "2", Title: "Song 2", URL: "url2"})
	q.Elapsed = 42

	err := q.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if q.CurrentIndex != 1 {
		t.Errorf("expected current index 1, got %d", q.CurrentIndex)
	}
	if q.Elapsed != 0 {
		t.Errorf("expected elapsed to be reset to 0, got %d", q.Elapsed)
	}
	if q.Status != StatusPlaying {
		t.Errorf("expected status playing, got %s", q.Status)
	}
}

func TestQueue_Next_EmptyQueue(t *testing.T) {
	q := NewQueue()

	err := q.Next()
	if err != ErrQueueEmpty {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestQueue_Next_EndOfQueue(t *testing.T) {
	q := NewQueue()
	q.Add(Song{ID: "1", Title: "Song 1", URL: "url1"})

	err := q.Next()
	if err != ErrNoNextSong {
		t.Errorf("expected ErrNoNextSong, got %v", err)
	}
}

func TestQueue_Prev_Success(t *testing.T) {
	q := NewQueue()
	q.Add(Song{ID: "1", Title: "Song 1", URL: "url1"})
	q.Add(Song{ID: "2", Title: "Song 2", URL: "url2"})
	q.Next()
	q.Elapsed = 30

	err := q.Prev()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if q.CurrentIndex != 0 {
		t.Errorf("expected current index 0, got %d", q.CurrentIndex)
	}
	if q.Elapsed != 0 {
		t.Errorf("expected elapsed to be reset to 0, got %d", q.Elapsed)
	}
	if q.Status != StatusPlaying {
		t.Errorf("expected status playing, got %s", q.Status)
	}
}

func TestQueue_Prev_EmptyQueue(t *testing.T) {
	q := NewQueue()

	err := q.Prev()
	if err != ErrQueueEmpty {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestQueue_Prev_AlreadyAtFirst(t *testing.T) {
	q := NewQueue()
	q.Add(Song{ID: "1", Title: "Song 1", URL: "url1"})

	err := q.Prev()
	if err == nil {
		t.Error("expected error when already at first song")
	}
}

func TestQueue_CurrentSong_Success(t *testing.T) {
	q := NewQueue()
	q.Add(Song{ID: "1", Title: "Song 1", URL: "url1"})

	song, err := q.CurrentSong()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if song.ID != "1" {
		t.Errorf("expected song ID '1', got '%s'", song.ID)
	}
}

func TestQueue_CurrentSong_EmptyQueue(t *testing.T) {
	q := NewQueue()

	_, err := q.CurrentSong()
	if err != ErrQueueEmpty {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestQueue_CurrentSong_InvalidIndex(t *testing.T) {
	q := NewQueue()
	q.Songs = []Song{{ID: "1", Title: "Song 1", URL: "url1"}}
	q.CurrentIndex = 5 // Out of range

	_, err := q.CurrentSong()
	if err == nil {
		t.Error("expected error for invalid current index")
	}
}

func TestQueue_IsValidTransition_IdleQueue(t *testing.T) {
	q := NewQueue() // CurrentIndex is -1

	if q.IsValidTransition(StatusPlaying) {
		t.Error("idle queue should not transition to playing")
	}
	if q.IsValidTransition(StatusPaused) {
		t.Error("idle queue should not transition to paused")
	}
	if !q.IsValidTransition(StatusIdle) {
		t.Error("idle queue should allow idle transition")
	}
}

func TestQueue_IsValidTransition_ActiveQueue(t *testing.T) {
	q := NewQueue()
	q.Add(Song{ID: "1", Title: "Song 1", URL: "url1"})

	if !q.IsValidTransition(StatusPlaying) {
		t.Error("active queue should allow playing transition")
	}
	if !q.IsValidTransition(StatusPaused) {
		t.Error("active queue should allow paused transition")
	}
	if !q.IsValidTransition(StatusIdle) {
		t.Error("active queue should allow idle transition")
	}
}

// --- User Tests ---

func TestUser_CanControlPlayback(t *testing.T) {
	tests := []struct {
		name string
		user User
		want bool
	}{
		{
			name: "host can control",
			user: User{DisplayName: "Host", Role: RoleHost},
			want: true,
		},
		{
			name: "admin can control",
			user: User{DisplayName: "Admin", Role: RoleAdmin},
			want: true,
		},
		{
			name: "guest cannot control",
			user: User{DisplayName: "Guest", Role: RoleGuest},
			want: false,
		},
		{
			name: "empty role cannot control",
			user: User{DisplayName: "Unknown", Role: ""},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.user.CanControlPlayback()
			if got != tt.want {
				t.Errorf("User.CanControlPlayback() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_CanHostPlayer(t *testing.T) {
	tests := []struct {
		name string
		user User
		want bool
	}{
		{
			name: "host can host player",
			user: User{DisplayName: "Host", Role: RoleHost},
			want: true,
		},
		{
			name: "admin cannot host player",
			user: User{DisplayName: "Admin", Role: RoleAdmin},
			want: false,
		},
		{
			name: "guest cannot host player",
			user: User{DisplayName: "Guest", Role: RoleGuest},
			want: false,
		},
		{
			name: "empty role cannot host player",
			user: User{DisplayName: "Unknown", Role: ""},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.user.CanHostPlayer()
			if got != tt.want {
				t.Errorf("User.CanHostPlayer() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- Activity Tests ---

func TestNewActivity(t *testing.T) {
	before := time.Now()
	a := NewActivity(ActivitySongAdded, "TestUser", "added a song")
	after := time.Now()

	if a.Type != ActivitySongAdded {
		t.Errorf("expected type %s, got %s", ActivitySongAdded, a.Type)
	}
	if a.User != "TestUser" {
		t.Errorf("expected user 'TestUser', got '%s'", a.User)
	}
	if a.Description != "added a song" {
		t.Errorf("expected description 'added a song', got '%s'", a.Description)
	}
	if a.Timestamp.Before(before) || a.Timestamp.After(after) {
		t.Errorf("timestamp %v is not between %v and %v", a.Timestamp, before, after)
	}
}

func TestNewActivity_AllTypes(t *testing.T) {
	types := []ActivityType{
		ActivitySongAdded,
		ActivitySongSkipped,
		ActivityPlayback,
		ActivityUserJoined,
	}

	for _, at := range types {
		t.Run(string(at), func(t *testing.T) {
			a := NewActivity(at, "user", "desc")
			if a.Type != at {
				t.Errorf("expected type %s, got %s", at, a.Type)
			}
		})
	}
}
