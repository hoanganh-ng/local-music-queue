package queue

import (
	"context"
	"errors"
	"local-music-queue/internal/domain/entity"
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
	if song.AddedBy != "Alice" {
		t.Errorf("expected AddedBy 'Alice', got '%s'", song.AddedBy)
	}
	if song.ID != "vid1" {
		t.Errorf("expected song ID 'vid1', got '%s'", song.ID)
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
	if song.ID != "vid2" {
		t.Errorf("expected song ID 'vid2', got '%s'", song.ID)
	}
	if song.Title != "Fast Song" {
		t.Errorf("expected title 'Fast Song', got '%s'", song.Title)
	}
	if song.AddedBy != "Bob" {
		t.Errorf("expected AddedBy 'Bob', got '%s'", song.AddedBy)
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
	// Setup: Create a queue with an existing song
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "vid1", Title: "Existing Song", URL: "https://youtube.com/watch?v=vid1"})

	repo := &mockQueueRepo{queue: q}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "vid1", Title: "Duplicate Song", URL: "https://youtube.com/watch?v=vid1"},
	}
	interactor := NewInteractor(repo, yt)

	// Attempt to add the same song again (same video ID)
	_, err := interactor.AddSong(context.Background(), "https://youtube.com/watch?v=vid1", "Bob", 2, nil)

	// Assert: Should return ErrSongAlreadyInQueue
	if !errors.Is(err, entity.ErrSongAlreadyInQueue) {
		t.Fatalf("expected ErrSongAlreadyInQueue, got %v", err)
	}

	// Assert: Queue length should be unchanged
	if len(repo.queue.Songs) != 1 {
		t.Errorf("expected queue to still have 1 song, got %d", len(repo.queue.Songs))
	}

	// Assert: Save should not be called for duplicate
	if repo.saveCalled {
		t.Error("expected Save NOT to be called for duplicate song")
	}
}

func TestAddSong_Duplicate_WithMetadata(t *testing.T) {
	// Setup: Create a queue with an existing song
	q := entity.NewQueue()
	q.Add(entity.Song{ID: "vid2", Title: "Existing Song", URL: "https://youtube.com/watch?v=vid2"})

	repo := &mockQueueRepo{queue: q}
	yt := &mockYouTubeService{}
	interactor := NewInteractor(repo, yt)

	// Attempt to add the same song via metadata (fast path)
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
	if len(repo.queue.Songs) != 1 {
		t.Errorf("expected queue to still have 1 song, got %d", len(repo.queue.Songs))
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
