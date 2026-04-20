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
	song *entity.Song
	err  error

	fetchCalled bool
}

func (m *mockYouTubeService) FetchMetadata(_ context.Context, _ string) (*entity.Song, error) {
	m.fetchCalled = true
	return m.song, m.err
}

// --- AddSong Tests ---

func TestAddSong_Success(t *testing.T) {
	repo := &mockQueueRepo{}
	yt := &mockYouTubeService{
		song: &entity.Song{ID: "vid1", Title: "Test Song", URL: "https://youtube.com/watch?v=vid1"},
	}
	interactor := NewInteractor(repo, yt)

	song, err := interactor.AddSong(context.Background(), "https://youtube.com/watch?v=vid1", "Alice")
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

func TestAddSong_FetchMetadataFails(t *testing.T) {
	repo := &mockQueueRepo{}
	yt := &mockYouTubeService{
		err: errors.New("yt-dlp failed"),
	}
	interactor := NewInteractor(repo, yt)

	_, err := interactor.AddSong(context.Background(), "bad-url", "Alice")
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

	_, err := interactor.AddSong(context.Background(), "url", "Alice")
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

	song, err := interactor.AddSong(context.Background(), "url", "Alice")
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
