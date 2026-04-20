package repository

import (
	"context"
	"local-music-queue/internal/domain/entity"
)

// QueueRepository defines the contract for persisting and retrieving the queue state.
type QueueRepository interface {
	Save(ctx context.Context, queue *entity.Queue) error
	Load(ctx context.Context) (*entity.Queue, error)
	AddActivity(ctx context.Context, activity entity.Activity) error
	GetActivities(ctx context.Context, limit int) ([]entity.Activity, error)
}
