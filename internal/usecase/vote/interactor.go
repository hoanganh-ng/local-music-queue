package vote

import (
	"context"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"sync"
	"time"
)

// VoteOutcome represents the result of a vote cast
type VoteOutcome struct {
	Session *entity.VoteSession
	Passed  bool
}

// Interactor handles vote-related business logic
type Interactor struct {
	queueRepo repository.QueueRepository
	userRepo  repository.UserRepository
	mu        sync.Mutex
	sessions  map[string]*entity.VoteSession
	expiry    time.Duration
}

// NewInteractor creates a new vote interactor
func NewInteractor(
	queueRepo repository.QueueRepository,
	userRepo repository.UserRepository,
	expiry time.Duration,
) *Interactor {
	if expiry == 0 {
		expiry = 30 * time.Second
	}
	return &Interactor{
		queueRepo: queueRepo,
		userRepo:  userRepo,
		sessions:  make(map[string]*entity.VoteSession),
		expiry:    expiry,
	}
}

// CastSkipVote casts a vote to skip the current song
func (i *Interactor) CastSkipVote(ctx context.Context, userID, connectedUsers int) (*VoteOutcome, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.queueRepo.Load(ctx)
	if err != nil {
		return nil, err
	}

	currentSong, err := queue.CurrentSong()
	if err != nil {
		return nil, err
	}

	sessionID := fmt.Sprintf("skip:%s", currentSong.ID)
	outcome, err := i.castVote(ctx, sessionID, entity.VoteTypeSkip, *currentSong, queue.CurrentIndex, userID, connectedUsers)
	if err != nil {
		return nil, err
	}

	if outcome.Passed {
		err = queue.Next()
		if err == entity.ErrNoNextSong {
			queue.Status = entity.StatusIdle
		} else if err != nil {
			return nil, err
		}

		if err := i.queueRepo.Save(ctx, queue); err != nil {
			return nil, err
		}

		displayName := i.displayName(ctx, userID)
		activity := entity.NewActivity(entity.ActivitySongSkipped, displayName, fmt.Sprintf("vote skipped \"%s\"", currentSong.Title))
		if err := i.queueRepo.AddActivity(ctx, activity); err != nil {
			return nil, err
		}
	}

	return outcome, nil
}

// CastPriorityVote casts a vote to prioritize a queued song
func (i *Interactor) CastPriorityVote(ctx context.Context, userID, songIndex, connectedUsers int) (*VoteOutcome, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	queue, err := i.queueRepo.Load(ctx)
	if err != nil {
		return nil, err
	}

	if songIndex < 0 || songIndex >= len(queue.Songs) {
		return nil, entity.ErrSongNotFound
	}

	if songIndex == queue.CurrentIndex {
		return nil, entity.ErrVoteOnCurrentSong
	}

	targetSong := queue.Songs[songIndex]
	sessionID := fmt.Sprintf("prioritize:%s", targetSong.ID)

	outcome, err := i.castVote(ctx, sessionID, entity.VoteTypePrioritize, targetSong, songIndex, userID, connectedUsers)
	if err != nil {
		return nil, err
	}

	if outcome.Passed {
		freshQueue, err := i.queueRepo.Load(ctx)
		if err != nil {
			return nil, err
		}

		resolvedIndex := i.resolveSongIndex(freshQueue, outcome.Session.SongID, outcome.Session.SongIndex)
		if resolvedIndex == -1 {
			return outcome, nil
		}

		freshQueue.Prioritize(resolvedIndex)

		if err := i.queueRepo.Save(ctx, freshQueue); err != nil {
			return nil, err
		}

		displayName := i.displayName(ctx, userID)
		activity := entity.NewActivity(entity.ActivityPlayback, displayName, fmt.Sprintf("vote prioritized \"%s\"", targetSong.Title))
		if err := i.queueRepo.AddActivity(ctx, activity); err != nil {
			return nil, err
		}
	}

	return outcome, nil
}

// castVote is the internal vote casting logic
func (i *Interactor) castVote(
	ctx context.Context,
	sessionID string,
	voteType entity.VoteType,
	song entity.Song,
	songIndex int,
	userID int,
	connectedUsers int,
) (*VoteOutcome, error) {
	i.evictExpired(ctx)

	session, exists := i.sessions[sessionID]
	if !exists {
		threshold := entity.MajorityThreshold(connectedUsers)
		session = entity.NewVoteSession(voteType, song, songIndex, threshold, i.expiry)
		i.sessions[sessionID] = session
	}

	if err := session.Cast(userID); err != nil {
		return nil, err
	}

	displayName := i.displayName(ctx, userID)
	voteTypeStr := string(voteType)
	activity := entity.NewActivity(
		entity.ActivityVoteCast,
		displayName,
		fmt.Sprintf("voted to %s \"%s\" (%d/%d)", voteTypeStr, song.Title, session.VoteCount(), session.Threshold),
	)
	if err := i.queueRepo.AddActivity(ctx, activity); err != nil {
		return nil, err
	}

	passed := session.IsPassed()
	if passed {
		delete(i.sessions, sessionID)
		passedActivity := entity.NewActivity(
			entity.ActivityVotePassed,
			"System",
			fmt.Sprintf("vote to %s \"%s\" passed", voteTypeStr, song.Title),
		)
		if err := i.queueRepo.AddActivity(ctx, passedActivity); err != nil {
			return nil, err
		}
	}

	return &VoteOutcome{Session: session, Passed: passed}, nil
}

// GetActiveSessions returns all active vote sessions
func (i *Interactor) GetActiveSessions() []*entity.VoteSession {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.evictExpired(context.Background())

	sessions := make([]*entity.VoteSession, 0, len(i.sessions))
	for _, session := range i.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// ExpireOldSessions removes expired sessions and returns them so callers
// can broadcast resolution events.
func (i *Interactor) ExpireOldSessions(ctx context.Context) []entity.ExpiredSession {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.evictExpired(ctx)
}

// evictExpired removes expired sessions, logs activities, and returns them.
func (i *Interactor) evictExpired(ctx context.Context) []entity.ExpiredSession {
	var expired []entity.ExpiredSession
	for sessionID, session := range i.sessions {
		if session.IsExpired() {
			activity := entity.NewActivity(
				entity.ActivityVoteExpired,
				"System",
				fmt.Sprintf("vote to %s \"%s\" expired", session.Type, session.SongTitle),
			)
			i.queueRepo.AddActivity(ctx, activity)
			delete(i.sessions, sessionID)
			expired = append(expired, entity.ExpiredSession{
				SessionID: sessionID,
				Activity:  activity,
			})
		}
	}
	return expired
}

// resolveSongIndex finds the current index of a song by ID
func (i *Interactor) resolveSongIndex(queue *entity.Queue, songID string, snapshotIndex int) int {
	if snapshotIndex >= 0 && snapshotIndex < len(queue.Songs) && queue.Songs[snapshotIndex].ID == songID {
		return snapshotIndex
	}

	for idx, song := range queue.Songs {
		if song.ID == songID {
			return idx
		}
	}

	return -1
}

// displayName fetches the user's display name
func (i *Interactor) displayName(ctx context.Context, userID int) string {
	user, err := i.userRepo.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return "Unknown"
	}
	return user.DisplayName
}
