package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
	"math/rand"
	"os/exec"
	"time"
)

var (
	ErrFetchFailed           = errors.New("yt-dlp fetch failed")
	ErrAllCandidatesExcluded = errors.New("all candidates excluded")
)

// YtDlpRelatedFetcher fetches related songs using yt-dlp's YouTube radio mix.
type YtDlpRelatedFetcher struct {
	MaxCandidates int
	commandRunner func(name string, args ...string) *exec.Cmd
}

// NewYtDlpRelatedFetcher creates a new related song fetcher.
func NewYtDlpRelatedFetcher(maxCandidates int) *YtDlpRelatedFetcher {
	if maxCandidates <= 0 {
		maxCandidates = 10
	}
	return &YtDlpRelatedFetcher{
		MaxCandidates: maxCandidates,
		commandRunner: exec.Command,
	}
}

// FetchRelated uses yt-dlp's YouTube radio mix to get candidates.
// Filters out IDs in exclude, returns the first non-excluded one (random order).
func (f *YtDlpRelatedFetcher) FetchRelated(
	ctx context.Context, videoID string, exclude []string,
) (*entity.Song, error) {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s&list=RD%s", videoID, videoID)

	cmd := f.commandRunner("yt-dlp",
		"--flat-playlist",
		"--dump-single-json",
		"--playlist-end", fmt.Sprintf("%d", f.MaxCandidates),
		url,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFetchFailed, stderr.String())
	}

	var data struct {
		Entries []YTDLPOutput `json:"entries"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		return nil, fmt.Errorf("%w: failed to parse JSON: %v", ErrFetchFailed, err)
	}

	if len(data.Entries) == 0 {
		return nil, ErrAllCandidatesExcluded
	}

	// Create exclude map for fast lookup
	excludeMap := make(map[string]bool)
	for _, id := range exclude {
		excludeMap[id] = true
	}

	// Shuffle entries for randomness
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(data.Entries), func(i, j int) {
		data.Entries[i], data.Entries[j] = data.Entries[j], data.Entries[i]
	})

	// Find first non-excluded entry
	for _, entry := range data.Entries {
		if !excludeMap[entry.ID] {
			thumbnail := entry.Thumbnail
			if thumbnail == "" {
				thumbnail = fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", entry.ID)
			}

			return &entity.Song{
				ID:        entry.ID,
				Title:     entry.Title,
				Artist:    entry.Uploader,
				Duration:  int(entry.Duration),
				Thumbnail: thumbnail,
				URL:       entry.WebpageURL,
				AddedBy:   entity.SystemUserID,
				AddedByID: 0,
			}, nil
		}
	}

	return nil, ErrAllCandidatesExcluded
}

// Ensure YtDlpRelatedFetcher implements domain.RelatedSongFetcher
var _ domain.RelatedSongFetcher = (*YtDlpRelatedFetcher)(nil)
