package activity

import (
	"context"
	"errors"
	"local-music-queue/internal/domain/entity"
	"testing"
)

// --- Mock QueueRepository ---

type mockQueueRepo struct {
	activities []entity.Activity
	addErr     error
	getErr     error
}

func (m *mockQueueRepo) Save(_ context.Context, _ *entity.Queue) error   { return nil }
func (m *mockQueueRepo) Load(_ context.Context) (*entity.Queue, error)   { return nil, nil }

func (m *mockQueueRepo) AddActivity(_ context.Context, a entity.Activity) error {
	if m.addErr != nil {
		return m.addErr
	}
	m.activities = append(m.activities, a)
	return nil
}

func (m *mockQueueRepo) GetActivities(_ context.Context, limit int) ([]entity.Activity, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if limit > len(m.activities) {
		limit = len(m.activities)
	}
	return m.activities[:limit], nil
}

// --- Tests ---

func TestGetRecentActivities_Success(t *testing.T) {
	repo := &mockQueueRepo{
		activities: []entity.Activity{
			entity.NewActivity(entity.ActivitySongAdded, "Alice", "added a song"),
			entity.NewActivity(entity.ActivitySongSkipped, "Bob", "skipped"),
		},
	}
	interactor := NewInteractor(repo)

	acts, err := interactor.GetRecentActivities(context.Background(), 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(acts) != 2 {
		t.Errorf("expected 2 activities, got %d", len(acts))
	}
}

func TestGetRecentActivities_Error(t *testing.T) {
	repo := &mockQueueRepo{getErr: errors.New("db error")}
	interactor := NewInteractor(repo)

	_, err := interactor.GetRecentActivities(context.Background(), 10)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLogActivity_Success(t *testing.T) {
	repo := &mockQueueRepo{}
	interactor := NewInteractor(repo)

	a := entity.NewActivity(entity.ActivityUserJoined, "Alice", "joined")
	err := interactor.LogActivity(context.Background(), a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.activities) != 1 {
		t.Errorf("expected 1 activity, got %d", len(repo.activities))
	}
}
