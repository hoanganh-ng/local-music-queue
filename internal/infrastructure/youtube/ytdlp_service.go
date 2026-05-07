package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"local-music-queue/internal/domain/entity"
	"os/exec"
	"sync"
	"time"
)

type cacheEntry struct {
	results   []*entity.SearchResult
	expiresAt time.Time
}

// YTDLPService implements the YouTubeService interface using yt-dlp.
type YTDLPService struct {
	binaryPath string
	cache      map[string]cacheEntry
	cacheMu    sync.RWMutex
	cacheTTL   time.Duration
}

// NewYTDLPService creates a new yt-dlp service.
func NewYTDLPService(binaryPath string) *YTDLPService {
	if binaryPath == "" {
		binaryPath = "yt-dlp"
	}
	return &YTDLPService{
		binaryPath: binaryPath,
		cache:      make(map[string]cacheEntry),
		cacheTTL:   5 * time.Minute,
	}
}

// YTDLPOutput represents the relevant parts of yt-dlp's JSON output.
type YTDLPOutput struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Uploader   string  `json:"uploader"`
	Duration   float64 `json:"duration"`
	Thumbnail  string  `json:"thumbnail"`
	WebpageURL string  `json:"webpage_url"`
}

// FetchMetadata uses yt-dlp to get song information.
func (s *YTDLPService) FetchMetadata(ctx context.Context, url string) (*entity.Song, error) {
	cmd := exec.CommandContext(ctx, s.binaryPath,
		"--print",
		"%(.{id,title,uploader,duration,thumbnail,webpage_url})#j",
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

	if data.Duration > 600 {
		return nil, fmt.Errorf("song duration exceeds 10 minutes limit (duration: %.0f seconds)", data.Duration)
	}

	return &entity.Song{
		ID:        data.ID,
		Title:     data.Title,
		Artist:    data.Uploader,
		Duration:  int(data.Duration),
		Thumbnail: data.Thumbnail,
		URL:       data.WebpageURL,
	}, nil
}

// SearchYouTube searches YouTube and returns search results.
func (s *YTDLPService) SearchYouTube(ctx context.Context, query string, maxResults int) ([]*entity.SearchResult, error) {
	cacheKey := fmt.Sprintf("%s:%d", query, maxResults)

	s.cacheMu.RLock()
	if entry, found := s.cache[cacheKey]; found && time.Now().Before(entry.expiresAt) {
		s.cacheMu.RUnlock()
		return entry.results, nil
	}
	s.cacheMu.RUnlock()

	searchQuery := fmt.Sprintf("ytsearch%d:%s", maxResults, query)

	cmd := exec.CommandContext(ctx, s.binaryPath,
		"--flat-playlist",
		"--print",
		"%(.{id,title,uploader,duration,thumbnail,webpage_url})#j",
		"--skip-download",
		searchQuery,
	)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("yt-dlp search error: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to run yt-dlp search: %w", err)
	}

	var results []*entity.SearchResult
	decoder := json.NewDecoder(bytes.NewReader(output))

	for decoder.More() {
		var data YTDLPOutput
		if err := decoder.Decode(&data); err != nil {
			continue
		}

		if data.Duration > 600 {
			continue
		}

		thumbnail := data.Thumbnail
		if thumbnail == "" {
			thumbnail = fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", data.ID)
		}

		results = append(results, &entity.SearchResult{
			ID:        data.ID,
			Title:     data.Title,
			Artist:    data.Uploader,
			Duration:  int(data.Duration),
			Thumbnail: thumbnail,
			URL:       data.WebpageURL,
		})
	}

	s.cacheMu.Lock()
	s.cache[cacheKey] = cacheEntry{
		results:   results,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()

	return results, nil
}
