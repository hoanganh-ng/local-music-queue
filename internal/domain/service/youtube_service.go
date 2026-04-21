package service

import (
	"context"
	"local-music-queue/internal/domain/entity"
)

// YouTubeService defines the contract for interacting with YouTube data.
type YouTubeService interface {
	FetchMetadata(ctx context.Context, url string) (*entity.Song, error)
	SearchYouTube(ctx context.Context, query string, maxResults int) ([]*entity.SearchResult, error)
}
