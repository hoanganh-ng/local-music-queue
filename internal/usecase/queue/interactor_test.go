package queue

import (
	"context"
	"errors"
	"local-music-queue/internal/domain/entity"
	"strings"
	"testing"
)

// --- Mock QueueRepository ---

type mockQueueRepo struct {
	queue      *entity.Queue
	loadErr    error
	saveErr    error
	activities []entity.Activity
	addActErr  error
	getActErr  error

	saveCalled    bool
	addActCalled  bool
}

func (m *mockQueueRepo) Save(_ context.Context, queue *entity.Queue) error {
	m.saveCalled = true
	if m.saveErr != nil {
		return m.saveErr
	}
	m.queue = queue
	return nil
}

func (m *mockQueueRepo) Load(_ context.Context) (*entity.Queue, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if m.queue == nil {
		return nil, errors.New("no queue state found")
	}
	return m.queue, nil
}

func (m *mockQueueRepo) AddActivity(_ context.Context, activity entity.Activity) error {
	m.addActCalled = true
	if m.addActErr != nil {
		return m.addActErr
	}
	m.activities = append(m.activities, activity)
	return nil
}

func (m *mockQueueRepo) GetActivities(_ context.Context, limit int) ([]entity.Activity, error) {
	if m.getActErr != nil {
		return nil, m.getActErr
	}
	if limit > len(m.activities) {
		limit = len(m.activities)
	}
	return m.activities[:limit], nil
}

// --- Mock YouTubeService ---

type mockYouTubeService struct {
	song    *entity.Song
	err     error
	results []*entity.SearchResult
	searchErr error

	fetchCalled  bool
	searchCalled bool
}

func (m *mockYouTubeService) FetchMetadata(_ context.Context, _ string) (*entity.Song, error) {
	m.fetchCalled = true
	return m.song, m.err
}

func (m *mockYouTubeService) SearchYouTube(_ context.Context, _ string, _ int) ([]*entity.SearchResult, error) {
	m.searchCalled = true
	return m.results, m.searchErr
}

// --- AddSong Tests ---

func TestAddSong_Success(t *testing.T) {
	repo := &mockQueueRepo{}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "vid1", Title: "Test Song", URL: "https://youtube.com/watch?v=vid1"},
	}
	interactor := NewInteractor(repo, yt)

	song, err := interactor.AddSong(context.Background(), "https://youtube.com/watch?v=vid1", "Alice", 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !yt.fetchCalled {
		t.Error("expected YouTubeService.FetchMetadata to be called")
	}
	if !repo.saveCalled {
		t.Error("expected QueueRepository.Save to be called")
	}
	if !repo.addActCalled {
		t.Error("expected QueueRepository.AddActivity to be called")
	}
	if song.Song.AddedBy != "Alice" {
		t.Errorf("expected AddedBy 'Alice', got '%s'", song.Song.AddedBy)
	}
	if song.Song.ID != "vid1" {
		t.Errorf("expected song ID 'vid1', got '%s'", song.Song.ID)
	}
	// Queue should have the song
	if repo.queue == nil || len(repo.queue.Songs) != 1 {
		t.Error("expected queue to contain 1 song after add")
	}
}

func TestAddSong_WithMetadata_SkipsFetch(t *testing.T) {
	repo := &mockQueueRepo{}
	yt := &mockYouTubeService{}
	interactor := NewInteractor(repo, yt)

	metadata := &entity.SearchResult{
		ID:        "vid2",
		Title:     "Fast Song",
		Artist:    "Fast Artist",
		Duration:  200,
		Thumbnail: "thumb.jpg",
		URL:       "https://youtube.com/watch?v=vid2",
	}

	song, err := interactor.AddSong(context.Background(), "https://youtube.com/watch?v=vid2", "Bob", 2, metadata)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if yt.fetchCalled {
		t.Error("expected FetchMetadata NOT to be called when metadata is provided")
	}
	if !repo.saveCalled {
		t.Error("expected Save to be called")
	}
	if song.Song.ID != "vid2" {
		t.Errorf("expected song ID 'vid2', got '%s'", song.Song.ID)
	}
	if song.Song.Title != "Fast Song" {
		t.Errorf("expected title 'Fast Song', got '%s'", song.Song.Title)
	}
	if song.Song.AddedBy != "Bob" {
		t.Errorf("expected AddedBy 'Bob', got '%s'", song.Song.AddedBy)
	}
}

func TestAddSong_FetchMetadataFails(t *testing.T) {
	repo := &mockQueueRepo{}
	yt := &mockYouTubeService{
		err: errors.New("yt-dlp failed"),
	}
	interactor := NewInteractor(repo, yt)

	_, err := interactor.AddSong(context.Background(), "bad-url", "Alice", 1, nil)
	if err == nil {
		t.Fatal("expected error when FetchMetadata fails")
	}
	if !errors.Is(err, yt.err) {
		// The error is wrapped, so check the message
		if err.Error() != "failed to fetch metadata: yt-dlp failed" {
			t.Errorf("unexpected error message: %v", err)
		}
	}
	if repo.saveCalled {
		t.Error("Save should not be called when FetchMetadata fails")
	}
}

func TestAddSong_SaveFails(t *testing.T) {
	repo := &mockQueueRepo{
		saveErr: errors.New("disk full"),
	}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "vid1", Title: "Test Song", URL: "url"},
	}
	interactor := NewInteractor(repo, yt)

	_, err := interactor.AddSong(context.Background(), "url", "Alice", 1, nil)
	if err == nil {
		t.Fatal("expected error when Save fails")
	}
}

func TestAddSong_LoadFails_CreatesNewQueue(t *testing.T) {
	repo := &mockQueueRepo{
		loadErr: errors.New("no queue state found"),
	}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "vid1", Title: "Test Song", URL: "url"},
	}
	interactor := NewInteractor(repo, yt)

	song, err := interactor.AddSong(context.Background(), "url", "Alice", 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if song == nil {
		t.Fatal("expected song to be returned")
	}
	// A new queue should have been created and saved
	if repo.queue == nil {
		t.Fatal("expected queue to be saved")
	}
	if len(repo.queue.Songs) != 1 {
		t.Errorf("expected 1 song in new queue, got %d", len(repo.queue.Songs))
	}
}

func TestAddSong_Duplicate(t *testing.T) {
	// Setup: Create a queue with an existing song in the upcoming queue
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "vid1", Title: "Current Song", URL: "https://youtube.com/watch?v=vid1"})
	q.Add(entity.Song{ID: "vid2", Title: "Upcoming Song", URL: "https://youtube.com/watch?v=vid2"})
	// CurrentIndex is 0 (vid1 is playing), vid2 is upcoming

	repo := &mockQueueRepo{queue: q}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "vid2", Title: "Duplicate Song", URL: "https://youtube.com/watch?v=vid2"},
	}
	interactor := NewInteractor(repo, yt)

	// Attempt to add vid2 again (it's in the upcoming queue)
	_, err := interactor.AddSong(context.Background(), "https://youtube.com/watch?v=vid2", "Bob", 2, nil)

	// Assert: Should return ErrSongAlreadyInQueue
	if !errors.Is(err, entity.ErrSongAlreadyInQueue) {
		t.Fatalf("expected ErrSongAlreadyInQueue, got %v", err)
	}

	// Assert: Queue length should be unchanged
	if len(repo.queue.Songs) != 2 {
		t.Errorf("expected queue to still have 2 songs, got %d", len(repo.queue.Songs))
	}

	// Assert: Save should not be called for duplicate
	if repo.saveCalled {
		t.Error("expected Save NOT to be called for duplicate song")
	}
}

func TestAddSong_CurrentlyPlayingSong_CanBeAddedAgain(t *testing.T) {
	// Setup: Create a queue with a currently playing song
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "vid1", Title: "Current Song", URL: "https://youtube.com/watch?v=vid1"})
	// CurrentIndex is 0 (vid1 is currently playing)

	repo := &mockQueueRepo{queue: q}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "vid1", Title: "Same Song Again", URL: "https://youtube.com/watch?v=vid1"},
	}
	interactor := NewInteractor(repo, yt)

	// Attempt to add vid1 again (it's currently playing, not in upcoming queue)
	song, err := interactor.AddSong(context.Background(), "https://youtube.com/watch?v=vid1", "Charlie", 3, nil)

	// Assert: Should succeed (currently playing song can be added again)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Assert: Song should be added
	if song == nil {
		t.Fatal("expected song to be returned")
	}

	// Assert: Queue should now have 2 songs
	if len(repo.queue.Songs) != 2 {
		t.Errorf("expected queue to have 2 songs, got %d", len(repo.queue.Songs))
	}

	// Assert: Save should be called
	if !repo.saveCalled {
		t.Error("expected Save to be called")
	}
}

func TestAddSong_Duplicate_WithMetadata(t *testing.T) {
	// Setup: Create a queue with an existing song in the upcoming queue
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "vid1", Title: "Current Song", URL: "https://youtube.com/watch?v=vid1"})
	q.Add(entity.Song{ID: "vid2", Title: "Upcoming Song", URL: "https://youtube.com/watch?v=vid2"})
	// CurrentIndex is 0 (vid1 is playing), vid2 is upcoming

	repo := &mockQueueRepo{queue: q}
	yt := &mockYouTubeService{}
	interactor := NewInteractor(repo, yt)

	// Attempt to add vid2 again via metadata (fast path)
	metadata := &entity.SearchResult{
		ID:        "vid2",
		Title:     "Duplicate via Metadata",
		Artist:    "Artist",
		Duration:  200,
		Thumbnail: "thumb.jpg",
		URL:       "https://youtube.com/watch?v=vid2",
	}

	_, err := interactor.AddSong(context.Background(), "https://youtube.com/watch?v=vid2", "Charlie", 3, metadata)

	// Assert: Should return ErrSongAlreadyInQueue
	if !errors.Is(err, entity.ErrSongAlreadyInQueue) {
		t.Fatalf("expected ErrSongAlreadyInQueue, got %v", err)
	}

	// Assert: FetchMetadata should not be called (metadata provided)
	if yt.fetchCalled {
		t.Error("expected FetchMetadata NOT to be called when metadata is provided")
	}

	// Assert: Queue length should be unchanged
	if len(repo.queue.Songs) != 2 {
		t.Errorf("expected queue to still have 2 songs, got %d", len(repo.queue.Songs))
	}
}

// --- SkipSong Tests ---

func TestSkipSong_Success(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "1", Title: "Song 1", URL: "url1"})
	q.Add(entity.Song{ID: "2", Title: "Song 2", URL: "url2"})
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	err := interactor.SkipSong(context.Background(), "Bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.queue.CurrentIndex != 1 {
		t.Errorf("expected current index 1 after skip, got %d", repo.queue.CurrentIndex)
	}
	if !repo.saveCalled {
		t.Error("expected Save to be called")
	}
	if !repo.addActCalled {
		t.Error("expected AddActivity to be called")
	}
}

func TestSkipSong_LoadFails(t *testing.T) {
	repo := &mockQueueRepo{loadErr: errors.New("db error")}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	err := interactor.SkipSong(context.Background(), "Bob")
	if err == nil {
		t.Fatal("expected error when Load fails")
	}
}

func TestSkipSong_NoNextSong(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "1", Title: "Song 1", URL: "url1"})
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	err := interactor.SkipSong(context.Background(), "Bob")
	if err != entity.ErrNoNextSong {
		t.Errorf("expected ErrNoNextSong, got %v", err)
	}
}

// --- GetState Tests ---

func TestGetState_Success(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "1", Title: "Song 1", URL: "url1"})
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	state, err := interactor.GetState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.Songs) != 1 {
		t.Errorf("expected 1 song, got %d", len(state.Songs))
	}
}

func TestGetState_LoadFails_ReturnsNewQueue(t *testing.T) {
	repo := &mockQueueRepo{loadErr: errors.New("db error")}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	state, err := interactor.GetState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(state.Songs) != 0 {
		t.Errorf("expected empty queue, got %d songs", len(state.Songs))
	}
}

// --- SetStatus Tests ---

func TestSetStatus_Success(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "1", Title: "Song 1", URL: "url1"})
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	err := interactor.SetStatus(context.Background(), "Alice", entity.StatusPaused)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.queue.Status != entity.StatusPaused {
		t.Errorf("expected status paused, got %s", repo.queue.Status)
	}
	if !repo.saveCalled {
		t.Error("expected Save to be called")
	}
}

func TestSetStatus_InvalidTransition(t *testing.T) {
	q := entity.NewQueue() // Empty queue, CurrentIndex == -1
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	err := interactor.SetStatus(context.Background(), "Alice", entity.StatusPlaying)
	if err == nil {
		t.Fatal("expected error for invalid status transition")
	}
}

func TestSetStatus_LoadFails(t *testing.T) {
	repo := &mockQueueRepo{loadErr: errors.New("db error")}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	err := interactor.SetStatus(context.Background(), "Alice", entity.StatusPlaying)
	if err == nil {
		t.Fatal("expected error when Load fails")
	}
}

// --- SearchYouTube Tests ---

func TestSearchYouTube_Success(t *testing.T) {
	results := []*entity.SearchResult{
		{ID: "1", Title: "Song 1", Artist: "Artist 1", Duration: 180, URL: "url1"},
		{ID: "2", Title: "Song 2", Artist: "Artist 2", Duration: 240, URL: "url2"},
	}
	yt := &mockYouTubeService{results: results}
	interactor := NewInteractor(&mockQueueRepo{}, yt)

	res, err := interactor.SearchYouTube(context.Background(), "test query")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 2 {
		t.Errorf("expected 2 results, got %d", len(res))
	}
	if !yt.searchCalled {
		t.Error("expected SearchYouTube to be called")
	}
}

func TestSearchYouTube_EmptyQuery(t *testing.T) {
	interactor := NewInteractor(&mockQueueRepo{}, &mockYouTubeService{})

	_, err := interactor.SearchYouTube(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSearchYouTube_ServiceError(t *testing.T) {
	yt := &mockYouTubeService{searchErr: errors.New("search failed")}
	interactor := NewInteractor(&mockQueueRepo{}, yt)

	_, err := interactor.SearchYouTube(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error when service fails")
	}
}

func TestRemoveSong_Usecase(t *testing.T) {
	setupQueue := func() (*entity.Queue, *mockQueueRepo, *Interactor) {
		q := entity.NewQueue()
		q.Add(entity.Song{ID: "vid0", Title: "Song 0", AddedByID: 10, AddedBy: "Guest1"})
		q.Add(entity.Song{ID: "vid1", Title: "Song 1", AddedByID: 20, AddedBy: "Guest2"})
		q.Add(entity.Song{ID: "vid2", Title: "Song 2", AddedByID: 10, AddedBy: "Guest1"})
		q.Add(entity.Song{ID: "vid3", Title: "Song 3", AddedByID: 20, AddedBy: "Guest2"})
		q.Add(entity.Song{ID: "vid4", Title: "Song 4", AddedByID: 0, AddedBy: "System"})
		q.CurrentIndex = 1
		q.Status = entity.StatusPlaying
		repo := &mockQueueRepo{queue: q}
		interactor := NewInteractor(repo, &mockYouTubeService{})
		return q, repo, interactor
	}

	ctx := context.Background()
	guest1 := &entity.User{ID: 10, Role: entity.RoleGuest, DisplayName: "Guest1"}
	host := &entity.User{ID: 1, Role: entity.RoleHost, DisplayName: "HostUser"}
	admin := &entity.User{ID: 2, Role: entity.RoleAdmin, DisplayName: "AdminUser"}

	t.Run("Guest removes own upcoming song", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		res, err := interactor.RemoveSong(ctx, guest1, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.RemovedIndex != 2 {
			t.Errorf("expected removed index 2, got %d", res.RemovedIndex)
		}
		if len(repo.queue.Songs) != 4 {
			t.Errorf("expected 4 songs remaining, got %d", len(repo.queue.Songs))
		}
	})

	t.Run("Guest cannot remove another user's song", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		_, err := interactor.RemoveSong(ctx, guest1, 3)
		if !errors.Is(err, ErrNotSongOwner) {
			t.Errorf("expected ErrNotSongOwner, got %v", err)
		}
		if repo.saveCalled {
			t.Error("Save should not have been called")
		}
	})

	t.Run("Guest cannot remove current song", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		_, err := interactor.RemoveSong(ctx, guest1, 1)
		if !errors.Is(err, ErrCannotRemoveSong) {
			t.Errorf("expected ErrCannotRemoveSong, got %v", err)
		}
		if repo.saveCalled {
			t.Error("Save should not have been called")
		}
	})

	t.Run("Guest cannot remove already-played song", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		_, err := interactor.RemoveSong(ctx, guest1, 0)
		if !errors.Is(err, ErrCannotRemoveSong) {
			t.Errorf("expected ErrCannotRemoveSong, got %v", err)
		}
		if repo.saveCalled {
			t.Error("Save should not have been called")
		}
	})

	t.Run("Guest cannot remove AddedByID == 0 song", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		_, err := interactor.RemoveSong(ctx, guest1, 4)
		if !errors.Is(err, ErrNotSongOwner) {
			t.Errorf("expected ErrNotSongOwner, got %v", err)
		}
		if repo.saveCalled {
			t.Error("Save should not have been called")
		}
	})

	t.Run("Host removes another user's song", func(t *testing.T) {
		_, _, interactor := setupQueue()
		res, err := interactor.RemoveSong(ctx, host, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.RemovedIndex != 3 {
			t.Errorf("expected removed index 3, got %d", res.RemovedIndex)
		}
	})

	t.Run("Admin removes another user's song", func(t *testing.T) {
		_, _, interactor := setupQueue()
		res, err := interactor.RemoveSong(ctx, admin, 3)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.RemovedIndex != 3 {
			t.Errorf("expected removed index 3, got %d", res.RemovedIndex)
		}
	})

	t.Run("Host removes current song preserving CurrentIndex, Status, and Elapsed semantics", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		repo.queue.Elapsed = 45 // Set dummy elapsed to check if it resets
		res, err := interactor.RemoveSong(ctx, host, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.RemovedIndex != 1 {
			t.Errorf("expected removed index 1, got %d", res.RemovedIndex)
		}
		if repo.queue.CurrentIndex != 1 {
			t.Errorf("expected CurrentIndex to be 1, got %d", repo.queue.CurrentIndex)
		}
		if repo.queue.Status != entity.StatusPlaying {
			t.Errorf("expected Status to be StatusPlaying, got %s", repo.queue.Status)
		}
		if repo.queue.Elapsed != 0 {
			t.Errorf("expected Elapsed to reset to 0, got %d", repo.queue.Elapsed)
		}
	})

	t.Run("Invalid index returns ErrInvalidIndex", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		_, err := interactor.RemoveSong(ctx, host, 10)
		if !errors.Is(err, ErrInvalidIndex) {
			t.Errorf("expected ErrInvalidIndex, got %v", err)
		}
		if repo.saveCalled {
			t.Error("Save should not have been called")
		}
	})

	t.Run("Authorization denial does not add activity", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		_, _ = interactor.RemoveSong(ctx, guest1, 3)
		if repo.addActCalled {
			t.Error("AddActivity should not have been called")
		}
	})

	t.Run("Save failure returns internal error", func(t *testing.T) {
		_, repo, interactor := setupQueue()
		repo.saveErr = errors.New("save error")
		_, err := interactor.RemoveSong(ctx, host, 2)
		if err == nil || !strings.Contains(err.Error(), "failed to save queue") {
			t.Errorf("expected save error, got %v", err)
		}
	})

	t.Run("Successful activity uses authenticated actor display name", func(t *testing.T) {
		_, _, interactor := setupQueue()
		res, err := interactor.RemoveSong(ctx, host, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Activity.User != "HostUser" {
			t.Errorf("expected activity user to be 'HostUser', got '%s'", res.Activity.User)
		}
	})
}

// --- Sprint 004: authoritative add-song snapshot + auto-queue staleness ---

// TestAddSong_AuthoritativeResult asserts the result returned by AddSong carries
// the post-mutation playback snapshot (Position, CurrentIndex, CurrentSong,
// Status, Elapsed, Activity) built inside the same lock as the queue write.
func TestAddSong_AuthoritativeResult(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "existing", Title: "Existing", URL: "url-existing"})
	// CurrentIndex 0, Status playing, Elapsed default 0.
	repo := &mockQueueRepo{queue: q}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "newvid", Title: "New", URL: "url-new"},
	}
	interactor := NewInteractor(repo, yt)

	res, err := interactor.AddSong(context.Background(), "url-new", "Alice", 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	if res.Position != 1 {
		t.Errorf("expected Position 1, got %d", res.Position)
	}
	if res.PreviousCurrentIndex != 0 {
		t.Errorf("expected PreviousCurrentIndex 0, got %d", res.PreviousCurrentIndex)
	}
	if res.CurrentIndex != 0 {
		t.Errorf("expected CurrentIndex 0, got %d", res.CurrentIndex)
	}
	if res.PlaybackAdvanced {
		t.Errorf("expected PlaybackAdvanced false (normal append), got true")
	}
	if res.CurrentSong == nil || res.CurrentSong.ID != "existing" {
		t.Errorf("expected CurrentSong 'existing', got %+v", res.CurrentSong)
	}
	if res.Status != entity.StatusPlaying {
		t.Errorf("expected Status playing, got %s", res.Status)
	}
	if res.Activity.Type != entity.ActivitySongAdded || res.Activity.User != "Alice" {
		t.Errorf("expected ActivitySongAdded for Alice, got %+v", res.Activity)
	}
}

// TestAddSong_AuthoritativeResult_FirstSong asserts that when the queue was
// empty, the snapshot reflects the just-promoted current song (queue.Add
// promotes the first inserted song; see entity.Queue.Add).
func TestAddSong_AuthoritativeResult_FirstSong(t *testing.T) {
	repo := &mockQueueRepo{}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "only", Title: "Only", URL: "url-only"},
	}
	interactor := NewInteractor(repo, yt)

	res, err := interactor.AddSong(context.Background(), "url-only", "Alice", 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Position != 0 {
		t.Errorf("expected Position 0, got %d", res.Position)
	}
	if res.PreviousCurrentIndex != -1 {
		t.Errorf("expected PreviousCurrentIndex -1, got %d", res.PreviousCurrentIndex)
	}
	if res.CurrentIndex != 0 {
		t.Errorf("expected CurrentIndex 0 (auto-promoted), got %d", res.CurrentIndex)
	}
	if !res.PlaybackAdvanced {
		t.Errorf("expected PlaybackAdvanced true (empty queue promotion), got false")
	}
	if res.CurrentSong == nil || res.CurrentSong.ID != "only" {
		t.Errorf("expected CurrentSong 'only', got %+v", res.CurrentSong)
	}
	if res.Status != entity.StatusPlaying {
		t.Errorf("expected Status playing (auto-start), got %s", res.Status)
	}
}

// TestAddSong_AuthoritativeResult_ExhaustedPaused advances playback from an
// exhausted-paused tail (queue.Add promotes the new song when current is
// last AND status is paused).
func TestAddSong_AuthoritativeResult_ExhaustedPaused(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "source", Title: "Source", URL: "url"})
	q.CurrentIndex = 0
	q.Status = entity.StatusPaused // exhausted
	repo := &mockQueueRepo{queue: q}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "after", Title: "After", URL: "url-after"},
	}
	interactor := NewInteractor(repo, yt)

	res, err := interactor.AddSong(context.Background(), "url-after", "Alice", 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.PreviousCurrentIndex != 0 {
		t.Errorf("expected PreviousCurrentIndex 0, got %d", res.PreviousCurrentIndex)
	}
	if res.CurrentIndex != 1 {
		t.Errorf("expected CurrentIndex 1 (promoted), got %d", res.CurrentIndex)
	}
	if !res.PlaybackAdvanced {
		t.Errorf("expected PlaybackAdvanced true (exhausted paused), got false")
	}
}

func TestAddAutoQueueSong_Success(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "source", Title: "Source", URL: "url"})
	// CurrentIndex 0, last song.
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	candidate := &entity.Song{ID: "candidate", Title: "Candidate", URL: "url-cand", AddedBy: "Auto-Queue"}
	res, err := interactor.AddAutoQueueSong(context.Background(), candidate, "source")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repo.saveCalled {
		t.Error("expected Save to be called")
	}
	if !repo.addActCalled {
		t.Error("expected AddActivity to be called")
	}
	if res.Position != 1 || res.CurrentIndex != 0 || res.Status != entity.StatusPlaying {
		t.Errorf("expected snapshot Position=1 CurrentIndex=0 Status=playing, got %+v", res)
	}
	if res.PreviousCurrentIndex != 0 {
		t.Errorf("expected PreviousCurrentIndex 0, got %d", res.PreviousCurrentIndex)
	}
	if res.PlaybackAdvanced {
		t.Errorf("expected PlaybackAdvanced false (auto-queue appends, doesn't advance), got true")
	}
	if res.Activity.User != "Auto-Queue" {
		t.Errorf("expected activity user 'Auto-Queue', got %s", res.Activity.User)
	}
}

// TestAddAutoQueueSong_LoadFailureIsOperational: ErrAutoQueueStale is reserved
// for predicate failures evaluated against a successfully-loaded queue. A
// repository load failure must surface as a wrapped operational error so the
// caller can distinguish an infra failure from a stale state.
func TestAddAutoQueueSong_LoadFailureIsOperational(t *testing.T) {
	repo := &mockQueueRepo{
		loadErr: errors.New("db unavailable"),
	}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	candidate := &entity.Song{ID: "candidate", Title: "Candidate", AddedBy: "Auto-Queue"}
	res, err := interactor.AddAutoQueueSong(context.Background(), candidate, "source")
	if err == nil {
		t.Fatal("expected error on repository load failure")
	}
	if errors.Is(err, ErrAutoQueueStale) {
		t.Errorf("load failure must NOT be reported as ErrAutoQueueStale, got %v", err)
	}
	if !strings.Contains(err.Error(), "failed to load queue for auto-queue revalidation") {
		t.Errorf("expected wrapped load error, got %v", err)
	}
	if res != nil {
		t.Error("expected nil result on operational load failure")
	}
	if repo.saveCalled {
		t.Error("Save must not be called on load failure")
	}
	if repo.addActCalled {
		t.Error("AddActivity must not be called on load failure")
	}
}

// TestAddAutoQueueSong_StaleSource: source song has already advanced
// (e.g. SkipSong landed between FetchRelated start and finish).
func TestAddAutoQueueSong_StaleSource(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "old-source", Title: "Old", URL: "url-old"})
	q.Add(entity.Song{ID: "new-current", Title: "New", URL: "url-new"})
	q.CurrentIndex = 1 // advanced; old-source is no longer current
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	candidate := &entity.Song{ID: "candidate", Title: "Candidate", AddedBy: "Auto-Queue"}
	res, err := interactor.AddAutoQueueSong(context.Background(), candidate, "old-source")
	if !errors.Is(err, ErrAutoQueueStale) {
		t.Fatalf("expected ErrAutoQueueStale, got %v", err)
	}
	if res != nil {
		t.Error("expected nil result on stale candidate")
	}
	if repo.saveCalled {
		t.Error("Save must not be called for stale candidate")
	}
	if repo.addActCalled {
		t.Error("AddActivity must not be called for stale candidate")
	}
	if len(repo.queue.Songs) != 2 {
		t.Errorf("queue unchanged expected (2 songs), got %d", len(repo.queue.Songs))
	}
}

// TestAddAutoQueueSong_StaleUpcomingExists: queue acquired an upcoming song
// between fetch start and finish (e.g. manual add).
func TestAddAutoQueueSong_StaleUpcomingExists(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "source", Title: "Source", URL: "url"})
	q.Add(entity.Song{ID: "manual-add", Title: "Manual", URL: "url-manual"})
	// CurrentIndex 0, but upcoming song now exists → trigger condition violated.
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	candidate := &entity.Song{ID: "candidate", Title: "Candidate", AddedBy: "Auto-Queue"}
	_, err := interactor.AddAutoQueueSong(context.Background(), candidate, "source")
	if !errors.Is(err, ErrAutoQueueStale) {
		t.Fatalf("expected ErrAutoQueueStale, got %v", err)
	}
	if repo.saveCalled {
		t.Error("Save must not be called when upcoming exists")
	}
	if repo.addActCalled {
		t.Error("AddActivity must not be called when upcoming exists")
	}
}

// TestAddAutoQueueSong_StaleDuplicate: candidate id already present in
// upcoming queue (concurrent enqueue) → stale.
func TestAddAutoQueueSong_StaleDuplicate(t *testing.T) {
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "source", Title: "Source", URL: "url"})
	repo := &mockQueueRepo{queue: q}
	interactor := NewInteractor(repo, &mockYouTubeService{})

	// Race: another goroutine already added "candidate" before we reach the
	// locked insertion. ContainsSong only inspects upcoming songs (after
	// CurrentIndex), so seed an upcoming song with the same id.
	q.Songs = append(q.Songs, entity.Song{ID: "candidate", Title: "Already Here"})
	// upcoming exists now too → expect stale on upcoming check (first match).
	candidate := &entity.Song{ID: "candidate", Title: "Candidate", AddedBy: "Auto-Queue"}
	_, err := interactor.AddAutoQueueSong(context.Background(), candidate, "source")
	if !errors.Is(err, ErrAutoQueueStale) {
		t.Fatalf("expected ErrAutoQueueStale, got %v", err)
	}
	if repo.saveCalled {
		t.Error("Save must not be called when candidate is duplicate or upcoming")
	}
}
