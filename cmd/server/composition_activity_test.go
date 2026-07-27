package main

// R09i corrective regression: normal server composition must select the
// explicit no-op room-activity repository for roomqueue, roomvote, and
// roomautoqueue, and must NOT select PostgresRoomActivityRepository.
// The check runs the real setupApp() wiring (not static source-text
// inspection) and reads the writer actually held by each interactor
// through the narrow ActivityWriterSeam accessors.

import (
	"os"
	"testing"

	"local-music-queue/internal/infrastructure/persistence"
)

func TestSetupApp_ComposesNoopRoomActivityWriter(t *testing.T) {
	scopedDSN := setupPostgresForTest(t)
	if os.Getenv("YTDLP_PATH") == "" {
		os.Setenv("YTDLP_PATH", "/bin/true")
	}
	os.Setenv("DATABASE_URL", scopedDSN)
	defer os.Unsetenv("DATABASE_URL")

	_, _, _, _, roomVoteInteractor, cleanup, err := setupApp()
	if err != nil {
		t.Fatalf("setupApp: %v", err)
	}
	defer cleanup()

	comp := composedActivityProducers
	if comp.roomQueue == nil || comp.roomVote == nil || comp.roomAutoQueue == nil {
		t.Fatal("setupApp did not capture the three activity-producing interactors")
	}
	if comp.roomVote != roomVoteInteractor {
		t.Error("captured roomvote interactor is not the one setupApp returned")
	}

	writers := map[string]interface{}{
		"roomqueue":     comp.roomQueue.ActivityWriterSeam(),
		"roomvote":      comp.roomVote.ActivityWriterSeam(),
		"roomautoqueue": comp.roomAutoQueue.ActivityWriterSeam(),
	}
	var noop *persistence.NoopRoomActivityRepository
	for name, w := range writers {
		if w == nil {
			t.Errorf("%s: no activity writer composed", name)
			continue
		}
		if _, isPG := w.(*persistence.PostgresRoomActivityRepository); isPG {
			t.Errorf("%s: PostgresRoomActivityRepository must not be composed pre-R14c", name)
			continue
		}
		got, ok := w.(*persistence.NoopRoomActivityRepository)
		if !ok {
			t.Errorf("%s: expected *persistence.NoopRoomActivityRepository, got %T", name, w)
			continue
		}
		// All three interactors must share the single writer instance
		// setupApp constructed.
		if noop == nil {
			noop = got
		} else if got != noop {
			t.Errorf("%s: expected the same no-op writer instance across all producers", name)
		}
	}
}
