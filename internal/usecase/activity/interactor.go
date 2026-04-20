package activity

import (
	"context"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
)

// Interactor handles activity-related logic.
type Interactor struct {
	repo repository.QueueRepository
}

// NewInteractor creates a new Activity Interactor.
func NewInteractor(repo repository.QueueRepository) *Interactor {
	return &Interactor{
		repo: repo,
	}
}

// GetRecentActivities returns the latest activity log entries.
func (i *Interactor) GetRecentActivities(ctx context.Context, limit int) ([]entity.Activity, error) {
	activities, err := i.repo.GetActivities(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch activities: %w", err)
	}
	return activities, nil
}

// LogActivity manually adds an activity entry.
func (i *Interactor) LogActivity(ctx context.Context, activity entity.Activity) error {
	return i.repo.AddActivity(ctx, activity)
}
