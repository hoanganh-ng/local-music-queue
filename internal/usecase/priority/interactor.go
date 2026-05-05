package priority

import (
	"context"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"strings"
	"sync"
	"time"
)

// Interactor handles priority-related business logic.
type Interactor struct {
	userRepo  repository.UserRepository
	queueRepo repository.QueueRepository
	mu        sync.RWMutex
}

// NewInteractor creates a new Priority Interactor.
func NewInteractor(userRepo repository.UserRepository, queueRepo repository.QueueRepository) *Interactor {
	return &Interactor{
		userRepo:  userRepo,
		queueRepo: queueRepo,
	}
}

// PrioritizeSong moves a user's song to the front of the queue.
func (i *Interactor) PrioritizeSong(ctx context.Context, userID int, songIndex int) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// 1. Load user and verify priority balance
	user, err := i.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	if !user.CanUsePriority() {
		return fmt.Errorf("insufficient priority balance")
	}

	// 2. Load queue
	queue, err := i.queueRepo.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	// 3. Verify ownership
	if songIndex < 0 || songIndex >= len(queue.Songs) {
		return fmt.Errorf("invalid song index")
	}

	song := queue.Songs[songIndex]
	if song.AddedByID != userID {
		return fmt.Errorf("can only prioritize your own songs")
	}

	// 4. Prioritize the song
	if err := queue.Prioritize(songIndex); err != nil {
		return err
	}

	// 5. Save queue
	if err := i.queueRepo.Save(ctx, queue); err != nil {
		return fmt.Errorf("failed to save queue: %w", err)
	}

	// 6. Deduct priority token
	if err := i.userRepo.DecrementPriority(ctx, userID); err != nil {
		return fmt.Errorf("failed to deduct priority: %w", err)
	}

	// 7. Log transaction
	user.PriorityBalance--
	_ = i.userRepo.LogPriorityTransaction(ctx, userID, song.ID, song.Title, "spent", -1, user.PriorityBalance)

	// 8. Log activity
	activity := entity.NewActivity(entity.ActivityPlayback, user.DisplayName,
		fmt.Sprintf("prioritized \"%s\"", song.Title))
	_ = i.queueRepo.AddActivity(ctx, activity)

	return nil
}

// CheckAndAwardDailyPriority checks if user should receive priority for today.
func (i *Interactor) CheckAndAwardDailyPriority(ctx context.Context, userID int) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	// Check last session date
	lastSession, err := i.userRepo.GetLastSessionDate(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to check session: %w", err)
	}

	// Only award if no session today
	if lastSession == nil || lastSession.Before(today) {
		// Record session FIRST - this acts as distributed lock via UNIQUE constraint
		if err := i.userRepo.RecordSession(ctx, userID, today); err != nil {
			// If conflict (session already exists), another request won the race - skip award
			if strings.Contains(err.Error(), "UNIQUE constraint") {
				return nil
			}
			return fmt.Errorf("failed to record session: %w", err)
		}

		// Award token ONLY after session successfully recorded
		if err := i.userRepo.IncrementPriority(ctx, userID); err != nil {
			return fmt.Errorf("failed to increment priority: %w", err)
		}

		// Log transaction
		user, _ := i.userRepo.GetUserByID(ctx, userID)
		_ = i.userRepo.LogPriorityTransaction(ctx, userID, "", "", "earned", 1, user.PriorityBalance)
	}

	return nil
}

// GetUserPriorityBalance returns current priority balance.
func (i *Interactor) GetUserPriorityBalance(ctx context.Context, userID int) (int, error) {
	user, err := i.userRepo.GetUserByID(ctx, userID)
	if err != nil {
		return 0, err
	}
	return user.PriorityBalance, nil
}
