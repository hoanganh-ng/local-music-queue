package vote

import (
	"context"
	"errors"
	"local-music-queue/internal/domain/entity"
	"testing"
	"time"
)

// --- Mock QueueRepository ---

type mockQueueRepo struct {
	queue      *entity.Queue
	loadErr    error
	saveErr    error
	activities []entity.Activity
	logActErr  error

	saveCalled   bool
	logActCalled bool
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

func (m *mockQueueRepo) LogActivity(_ context.Context, activity entity.Activity) error {
	m.logActCalled = true
	if m.logActErr != nil {
		return m.logActErr
	}
	m.activities = append(m.activities, activity)
	return nil
}

func (m *mockQueueRepo) AddActivity(_ context.Context, activity entity.Activity) error {
	return m.LogActivity(context.Background(), activity)
}

func (m *mockQueueRepo) GetActivities(_ context.Context, limit int) ([]entity.Activity, error) {
	if limit > len(m.activities) {
		limit = len(m.activities)
	}
	return m.activities[:limit], nil
}

// --- Mock UserRepository ---

type mockUserRepo struct {
	users  map[int]*entity.User
	getErr error
}

func (m *mockUserRepo) GetUserByID(_ context.Context, userID int) (*entity.User, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	user, exists := m.users[userID]
	if !exists {
		return nil, errors.New("user not found")
	}
	return user, nil
}

func (m *mockUserRepo) CreateUser(_ context.Context, user *entity.User) error {
	return nil
}

func (m *mockUserRepo) UpdateUser(_ context.Context, user *entity.User) error {
	return nil
}

func (m *mockUserRepo) GetAllUsers(_ context.Context) ([]*entity.User, error) {
	return nil, nil
}

func (m *mockUserRepo) GetUserByEmail(_ context.Context, email string) (*entity.User, error) {
	return nil, nil
}

func (m *mockUserRepo) DecrementPriority(_ context.Context, userID int) error {
	return nil
}

func (m *mockUserRepo) IncrementPriority(_ context.Context, userID int) error {
	return nil
}

func (m *mockUserRepo) RecordSession(_ context.Context, userID int, sessionDate time.Time) error {
	return nil
}

func (m *mockUserRepo) GetLastSessionDate(_ context.Context, userID int) (*time.Time, error) {
	return nil, nil
}

func (m *mockUserRepo) LogPriorityTransaction(_ context.Context, userID int, songID, songTitle, txType string, amount, balanceAfter int) error {
	return nil
}

// --- Helper Functions ---

func createTestQueue() *entity.Queue {
	return &entity.Queue{
		Songs: []entity.Song{
			{ID: "song1", Title: "Song 1", URL: "https://youtube.com/watch?v=song1"},
			{ID: "song2", Title: "Song 2", URL: "https://youtube.com/watch?v=song2"},
			{ID: "song3", Title: "Song 3", URL: "https://youtube.com/watch?v=song3"},
		},
		CurrentIndex: 0,
		Status:       entity.StatusPlaying,
	}
}

// --- CastSkipVote Tests ---

func TestCastSkipVote_FirstVoteCreatesSession(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	outcome, err := interactor.CastSkipVote(context.Background(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Passed {
		t.Error("expected vote not to pass with only 1 vote out of 3 users")
	}

	if outcome.Session.VoteCount() != 1 {
		t.Errorf("expected vote count 1, got %d", outcome.Session.VoteCount())
	}

	if outcome.Session.Threshold != 2 {
		t.Errorf("expected threshold 2, got %d", outcome.Session.Threshold)
	}

	if !repo.logActCalled {
		t.Error("expected activity to be logged")
	}
}

func TestCastSkipVote_SecondVoteJoinsSession(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
			2: {ID: 2, DisplayName: "Bob"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastSkipVote(context.Background(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error on first vote: %v", err)
	}

	outcome, err := interactor.CastSkipVote(context.Background(), 2, 3)
	if err != nil {
		t.Fatalf("unexpected error on second vote: %v", err)
	}

	if outcome.Session.VoteCount() != 2 {
		t.Errorf("expected vote count 2, got %d", outcome.Session.VoteCount())
	}

	if !outcome.Passed {
		t.Error("expected vote to pass with 2 votes (threshold 2)")
	}
}

func TestCastSkipVote_ThresholdReachedAdvancesQueue(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
			2: {ID: 2, DisplayName: "Bob"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	interactor.CastSkipVote(context.Background(), 1, 3)
	outcome, err := interactor.CastSkipVote(context.Background(), 2, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !outcome.Passed {
		t.Fatal("expected vote to pass")
	}

	if !repo.saveCalled {
		t.Error("expected queue to be saved")
	}

	if repo.queue.CurrentIndex != 1 {
		t.Errorf("expected current index to advance to 1, got %d", repo.queue.CurrentIndex)
	}
}

func TestCastSkipVote_DuplicateVoteReturnsError(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastSkipVote(context.Background(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error on first vote: %v", err)
	}

	_, err = interactor.CastSkipVote(context.Background(), 1, 3)
	if err != entity.ErrAlreadyVoted {
		t.Errorf("expected ErrAlreadyVoted, got %v", err)
	}
}

func TestCastSkipVote_EmptyQueueReturnsError(t *testing.T) {
	queue := &entity.Queue{
		Songs:        []entity.Song{},
		CurrentIndex: -1,
		Status:       entity.StatusIdle,
	}
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastSkipVote(context.Background(), 1, 3)
	if err != entity.ErrQueueEmpty {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestCastSkipVote_PassedSessionRemovedFromActiveSessions(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
			2: {ID: 2, DisplayName: "Bob"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	interactor.CastSkipVote(context.Background(), 1, 3)
	interactor.CastSkipVote(context.Background(), 2, 3)

	sessions := interactor.GetActiveSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 active sessions after vote passed, got %d", len(sessions))
	}
}

// --- CastPriorityVote Tests ---

func TestCastPriorityVote_FirstVoteCreatesSession(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	outcome, err := interactor.CastPriorityVote(context.Background(), 1, 2, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Passed {
		t.Error("expected vote not to pass with only 1 vote")
	}

	if outcome.Session.VoteCount() != 1 {
		t.Errorf("expected vote count 1, got %d", outcome.Session.VoteCount())
	}
}

func TestCastPriorityVote_ThresholdReachedPrioritizesSong(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
			2: {ID: 2, DisplayName: "Bob"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	interactor.CastPriorityVote(context.Background(), 1, 2, 3)
	outcome, err := interactor.CastPriorityVote(context.Background(), 2, 2, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !outcome.Passed {
		t.Fatal("expected vote to pass")
	}

	if !repo.saveCalled {
		t.Error("expected queue to be saved")
	}

	if repo.queue.Songs[1].ID != "song3" {
		t.Errorf("expected song3 to be at index 1, got %s", repo.queue.Songs[1].ID)
	}
}

func TestCastPriorityVote_CurrentSongReturnsError(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastPriorityVote(context.Background(), 1, 0, 3)
	if err != entity.ErrVoteOnCurrentSong {
		t.Errorf("expected ErrVoteOnCurrentSong, got %v", err)
	}
}

func TestCastPriorityVote_OutOfRangeIndexReturnsError(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastPriorityVote(context.Background(), 1, 10, 3)
	if err != entity.ErrSongNotFound {
		t.Errorf("expected ErrSongNotFound, got %v", err)
	}
}

func TestCastPriorityVote_DuplicateVoteReturnsError(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastPriorityVote(context.Background(), 1, 2, 3)
	if err != nil {
		t.Fatalf("unexpected error on first vote: %v", err)
	}

	_, err = interactor.CastPriorityVote(context.Background(), 1, 2, 3)
	if err != entity.ErrAlreadyVoted {
		t.Errorf("expected ErrAlreadyVoted, got %v", err)
	}
}

func TestCastVote_SkipAndPrioritySessionsCoexist(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastSkipVote(context.Background(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error on skip vote: %v", err)
	}

	_, err = interactor.CastPriorityVote(context.Background(), 1, 2, 3)
	if err != nil {
		t.Fatalf("unexpected error on priority vote: %v", err)
	}

	sessions := interactor.GetActiveSessions()
	if len(sessions) != 2 {
		t.Errorf("expected 2 active sessions, got %d", len(sessions))
	}
}

// --- ExpireOldSessions Tests ---

func TestExpireOldSessions_RemovesExpiredSessions(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	// Cast a vote to create a session
	_, err := interactor.CastSkipVote(context.Background(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Manually expire the session by setting its ExpiresAt to the past
	interactor.mu.Lock()
	for _, session := range interactor.sessions {
		session.ExpiresAt = time.Now().Add(-1 * time.Second)
	}
	interactor.mu.Unlock()

	interactor.ExpireOldSessions(context.Background())

	sessions := interactor.GetActiveSessions()
	if len(sessions) != 0 {
		t.Errorf("expected 0 active sessions after expiry, got %d", len(sessions))
	}

	if !repo.logActCalled {
		t.Error("expected activity to be logged for expired session")
	}

	hasExpiredActivity := false
	for _, act := range repo.activities {
		if act.Type == entity.ActivityVoteExpired {
			hasExpiredActivity = true
			break
		}
	}
	if !hasExpiredActivity {
		t.Error("expected ActivityVoteExpired to be logged")
	}
}

func TestExpireOldSessions_KeepsLiveSessions(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastSkipVote(context.Background(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	interactor.ExpireOldSessions(context.Background())

	sessions := interactor.GetActiveSessions()
	if len(sessions) != 1 {
		t.Errorf("expected 1 active session, got %d", len(sessions))
	}
}

// --- Activity Logging Tests ---

func TestCastSkipVote_LogsActivityVoteCast(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	_, err := interactor.CastSkipVote(context.Background(), 1, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.activities) == 0 {
		t.Fatal("expected at least one activity to be logged")
	}

	if repo.activities[0].Type != entity.ActivityVoteCast {
		t.Errorf("expected ActivityVoteCast, got %s", repo.activities[0].Type)
	}
}

func TestCastSkipVote_PassingVoteLogsActivityVotePassed(t *testing.T) {
	queue := createTestQueue()
	repo := &mockQueueRepo{queue: queue}
	userRepo := &mockUserRepo{
		users: map[int]*entity.User{
			1: {ID: 1, DisplayName: "Alice"},
			2: {ID: 2, DisplayName: "Bob"},
		},
	}
	interactor := NewInteractor(repo, userRepo, 30*time.Second)

	interactor.CastSkipVote(context.Background(), 1, 3)
	_, err := interactor.CastSkipVote(context.Background(), 2, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasPassedActivity := false
	for _, act := range repo.activities {
		if act.Type == entity.ActivityVotePassed {
			hasPassedActivity = true
			break
		}
	}
	if !hasPassedActivity {
		t.Error("expected ActivityVotePassed to be logged")
	}
}
