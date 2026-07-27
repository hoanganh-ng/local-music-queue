package persistence

import (
	"context"
	"testing"

	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// The no-op writer must satisfy the same contract the PostgreSQL
// implementation does so cmd/server can inject it wherever a
// repository.RoomActivityRepository is expected.
var _ repository.RoomActivityRepository = (*NoopRoomActivityRepository)(nil)

func TestNoopRoomActivityRepository_AddActivityReturnsNil(t *testing.T) {
	repo := NewNoopRoomActivityRepository()
	act := entity.NewActivity(entity.ActivitySongAdded, "tester", `added "Song"`)
	if err := repo.AddActivity(context.Background(), 42, act); err != nil {
		t.Fatalf("AddActivity: expected nil error, got %v", err)
	}
}

func TestNoopRoomActivityRepository_GetActivitiesReturnsNonNilEmpty(t *testing.T) {
	repo := NewNoopRoomActivityRepository()

	// The empty result must hold regardless of prior writes and for
	// any limit, including non-positive ones.
	_ = repo.AddActivity(context.Background(), 7, entity.NewActivity(entity.ActivityPlayback, "tester", "cleared the queue"))

	for _, limit := range []int{-1, 0, 1, 50} {
		got, err := repo.GetActivities(context.Background(), 7, limit)
		if err != nil {
			t.Fatalf("GetActivities(limit=%d): expected nil error, got %v", limit, err)
		}
		if got == nil {
			t.Fatalf("GetActivities(limit=%d): expected non-nil slice, got nil", limit)
		}
		if len(got) != 0 {
			t.Fatalf("GetActivities(limit=%d): expected empty slice, got %d entries", limit, len(got))
		}
	}
}
