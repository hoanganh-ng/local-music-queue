package youtube

import (
	"context"
	"local-music-queue/internal/domain/entity"
	"os/exec"
	"testing"
)

func TestNewYtDlpRelatedFetcher_DefaultBinaryPath(t *testing.T) {
	fetcher := NewYtDlpRelatedFetcher("", 10)
	if fetcher.binaryPath != "yt-dlp" {
		t.Fatalf("expected default binaryPath=yt-dlp, got %s", fetcher.binaryPath)
	}
}

func TestYtDlpRelatedFetcher_FetchRelated(t *testing.T) {
	t.Run("returns first non-excluded entry", func(t *testing.T) {
		fetcher := NewYtDlpRelatedFetcher("", 10)

		mockJSON := `{
			"entries": [
				{"id": "dQw4w9WgXcQ", "title": "Song 1", "uploader": "Artist 1", "duration": 180, "webpage_url": "https://youtube.com/watch?v=dQw4w9WgXcQ"},
				{"id": "9bZkp7q19f0", "title": "Song 2", "uploader": "Artist 2", "duration": 200, "webpage_url": "https://youtube.com/watch?v=9bZkp7q19f0"},
				{"id": "kJQP7kiw5Fk", "title": "Song 3", "uploader": "Artist 3", "duration": 220, "webpage_url": "https://youtube.com/watch?v=kJQP7kiw5Fk"}
			]
		}`

		fetcher.commandRunner = func(name string, args ...string) *exec.Cmd {
			cmd := exec.Command("echo", mockJSON)
			return cmd
		}

		song, err := fetcher.FetchRelated(context.Background(), "test_video", []string{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if song == nil {
			t.Fatal("expected song, got nil")
		}
		if song.AddedBy != entity.SystemUserID {
			t.Errorf("expected AddedBy=%s, got %s", entity.SystemUserID, song.AddedBy)
		}
	})

	t.Run("returns ErrAllCandidatesExcluded when all excluded", func(t *testing.T) {
		fetcher := NewYtDlpRelatedFetcher("", 10)

		mockJSON := `{
			"entries": [
				{"id": "dQw4w9WgXcQ", "title": "Song 1", "uploader": "Artist 1", "duration": 180, "webpage_url": "https://youtube.com/watch?v=dQw4w9WgXcQ"},
				{"id": "9bZkp7q19f0", "title": "Song 2", "uploader": "Artist 2", "duration": 200, "webpage_url": "https://youtube.com/watch?v=9bZkp7q19f0"}
			]
		}`

		fetcher.commandRunner = func(name string, args ...string) *exec.Cmd {
			cmd := exec.Command("echo", mockJSON)
			return cmd
		}

		exclude := []string{"dQw4w9WgXcQ", "9bZkp7q19f0"}
		_, err := fetcher.FetchRelated(context.Background(), "test_video", exclude)
		if err != ErrAllCandidatesExcluded {
			t.Errorf("expected ErrAllCandidatesExcluded, got %v", err)
		}
	})

	t.Run("returns ErrFetchFailed on command error", func(t *testing.T) {
		fetcher := NewYtDlpRelatedFetcher("", 10)

		fetcher.commandRunner = func(name string, args ...string) *exec.Cmd {
			cmd := exec.Command("false")
			return cmd
		}

		_, err := fetcher.FetchRelated(context.Background(), "test_video", []string{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("sets AddedBy to SystemUserID", func(t *testing.T) {
		fetcher := NewYtDlpRelatedFetcher("", 10)

		mockJSON := `{
			"entries": [
				{"id": "dQw4w9WgXcQ", "title": "Song 1", "uploader": "Artist 1", "duration": 180, "webpage_url": "https://youtube.com/watch?v=dQw4w9WgXcQ"}
			]
		}`

		fetcher.commandRunner = func(name string, args ...string) *exec.Cmd {
			cmd := exec.Command("echo", mockJSON)
			return cmd
		}

		song, err := fetcher.FetchRelated(context.Background(), "test_video", []string{})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if song.AddedBy != entity.SystemUserID {
			t.Errorf("expected AddedBy=%s, got %s", entity.SystemUserID, song.AddedBy)
		}
		if song.AddedByID != 0 {
			t.Errorf("expected AddedByID=0, got %d", song.AddedByID)
		}
	})
}
