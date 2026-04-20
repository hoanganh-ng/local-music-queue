package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"os/exec"
	"time"
)

// YTDLPService implements the YouTubeService interface using yt-dlp.
type YTDLPService struct {
	binaryPath string
}

// NewYTDLPService creates a new yt-dlp service.
func NewYTDLPService(binaryPath string) *YTDLPService {
	if binaryPath == "" {
		binaryPath = "yt-dlp"
	}
	return &YTDLPService{
		binaryPath: binaryPath,
	}
}

// YTDLPOutput represents the relevant parts of yt-dlp's JSON output.
type YTDLPOutput struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Uploader  string  `json:"uploader"`
	Duration  float64 `json:"duration"`
	Thumbnail string  `json:"thumbnail"`
	WebpageURL string `json:"webpage_url"`
}

// FetchMetadata uses yt-dlp to get song information.
func (s *YTDLPService) FetchMetadata(ctx context.Context, url string) (*entity.Song, error) {
	cmd := exec.CommandContext(ctx, s.binaryPath,
		"--print-json",
		"--skip-download",
		url,
	)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("yt-dlp error: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run yt-dlp: %w", err)
	}

	var data YTDLPOutput
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, fmt.Errorf("failed to parse yt-dlp output: %w", err)
	}

	return &entity.Song{
		ID:        data.ID,
		Title:     data.Title,
		Artist:    data.Uploader,
		Duration:  time.Duration(data.Duration * float64(time.Second)),
		Thumbnail: data.Thumbnail,
		URL:       data.WebpageURL,
	}, nil
}
