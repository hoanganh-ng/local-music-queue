package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	delivery "local-music-queue/internal/delivery/http"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/domain"
	"local-music-queue/internal/domain/entity"
	"local-music-queue/internal/domain/repository"
	"local-music-queue/internal/infrastructure/config"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/infrastructure/session"
	"local-music-queue/internal/infrastructure/youtube"
	usecaseActivity "local-music-queue/internal/usecase/activity"
	usecaseAuth "local-music-queue/internal/usecase/auth"
	usecaseAutoQueue "local-music-queue/internal/usecase/autoqueue"
	usecasePriority "local-music-queue/internal/usecase/priority"
	usecaseQueue "local-music-queue/internal/usecase/queue"
	usecaseVote "local-music-queue/internal/usecase/vote"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	mux, cfg, err := setupApp()
	if err != nil {
		log.Fatalf("Failed to setup application: %v", err)
	}

	// Start Server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: requestLogger(enableCORS(mux)),
	}

	// Check if we should serve HTTPS
	certFile := os.Getenv("CERT_FILE")
	keyFile := os.Getenv("KEY_FILE")

	if certFile != "" && keyFile != "" {
		log.Printf("Server listening on https://0.0.0.0:%s", cfg.Port)
		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	} else {
		log.Printf("Server listening on http://localhost:%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}
}

func setupApp() (*http.ServeMux, *config.Config, error) {
	// 1. Load configuration
	cfg := config.Load()
	log.Printf("Starting Local Music Queue server on port %s", cfg.Port)

	// Load Google OAuth config from environment
	googleClientID := os.Getenv("GOOGLE_CLIENT_ID")
	if googleClientID == "" {
		log.Println("Warning: GOOGLE_CLIENT_ID not set")
	}

	hostEmailsStr := os.Getenv("HOST_EMAILS")
	adminEmailsStr := os.Getenv("ADMIN_EMAILS")

	var hostEmails []string
	var adminEmails []string

	if hostEmailsStr != "" {
		hostEmails = strings.Split(hostEmailsStr, ",")
		for i := range hostEmails {
			hostEmails[i] = strings.TrimSpace(hostEmails[i])
		}
	}

	if adminEmailsStr != "" {
		adminEmails = strings.Split(adminEmailsStr, ",")
		for i := range adminEmails {
			adminEmails[i] = strings.TrimSpace(adminEmails[i])
		}
	}

	log.Printf("Host emails: %v", hostEmails)
	log.Printf("Admin emails: %v", adminEmails)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}

	// 2. Initialize Infrastructure
	// Backend selection: PostgreSQL when DATABASE_URL is set, SQLite otherwise.
	// The SQLite fallback is retained for R02/R03 per ADR 002 §13.
	queueRepo, userRepo, autoQueueRepo, dbHandle, err := initRepositories(cfg)
	if err != nil {
		return nil, nil, err
	}
	if dbHandle != nil {
		defer dbHandle.Close()
	}

	// Run embedded PostgreSQL schema migrations defensively on backend startup.
	// The db-init Compose job is authoritative; this is belt-and-braces for
	// `go run ./cmd/server` and CI. ErrNoChange is not an error.
	if cfg.DatabaseURL != "" {
		if err := persistence.RunEmbeddedMigrationsUp(dbHandle); err != nil {
			return nil, nil, err
		}
	}

	ytService := youtube.NewYTDLPService(cfg.YTDLPPath)

	// Initialize Session Store
	sessionClock := usecaseAuth.RealClock{}
	sessionStore := session.NewInMemoryStore(sessionClock)

	// 3. Initialize Usecases
	qInteractor := usecaseQueue.NewInteractor(queueRepo, ytService)
	authInteractor := usecaseAuth.NewInteractor(userRepo, googleClientID, hostEmails, adminEmails, sessionStore, sessionClock)
	actInteractor := usecaseActivity.NewInteractor(queueRepo)
	priorityInteractor := usecasePriority.NewInteractor(userRepo, queueRepo)
	voteInteractor := usecaseVote.NewInteractor(queueRepo, userRepo, 0) // 0 = default 30s expiry

	// Initialize auto-queue components
	ytRelatedFetcher := youtube.NewYtDlpRelatedFetcher(cfg.YTDLPPath, 10)
	autoQueueInteractor := usecaseAutoQueue.NewInteractor(autoQueueRepo, queueRepo, ytRelatedFetcher)

	// Wire auto-queue into queue interactor
	qInteractor.SetAutoQueueTrigger(autoQueueInteractor)

	// Wire auto-queue callbacks to avoid race conditions.
	// Adapter translates between queue.AddAutoQueueSong (which owns the lock
	// and returns queue.AddSongResult / queue.ErrAutoQueueStale) and the
	// transport types declared in the autoqueue package, so autoqueue stays
	// independent of usecase/queue.
	autoQueueInteractor.SetAddAutoQueueSongFunc(func(ctx context.Context, song *entity.Song, expectedSourceSongID string) (*usecaseAutoQueue.AddSongResult, error) {
		res, err := qInteractor.AddAutoQueueSong(ctx, song, expectedSourceSongID)
		if err != nil {
			if errors.Is(err, usecaseQueue.ErrAutoQueueStale) {
				return nil, usecaseAutoQueue.ErrAutoQueueStale
			}
			return nil, err
		}
		return &usecaseAutoQueue.AddSongResult{
			Song:         res.Song,
			Position:     res.Position,
			CurrentIndex: res.CurrentIndex,
			CurrentSong:  res.CurrentSong,
			Status:       res.Status,
			Elapsed:      res.Elapsed,
			Activity:     res.Activity,
		}, nil
	})

	// 4. Initialize Delivery with queue state callback
	hub := ws.NewHub(qInteractor.GetState)
	go hub.Run() // Start WebSocket hub loop

	// Wire auto-queue broadcaster to WS hub
	autoQueueInteractor.SetBroadcaster(hub.Broadcast)

	// Wire vote interactor into hub
	hub.SetVoteInteractor(voteInteractor)

	// Wire priority interactor into hub
	hub.SetPriorityInteractor(priorityInteractor)

	handlers := delivery.NewHandlers(qInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub)
	autoQueueHandlers := delivery.NewAutoQueueHandlers(autoQueueInteractor, hub)

	// 5. Setup Routes
	mux := http.NewServeMux()

	// HTTP API
	mux.HandleFunc("POST /api/auth/google", handlers.HandleGoogleLogin)
	mux.HandleFunc("POST /api/auth", handlers.HandleLogin) // Deprecated
	mux.HandleFunc("GET /api/queue", handlers.HandleGetQueue)
	mux.HandleFunc("POST /api/queue/add", handlers.HandleAddSong)
	mux.HandleFunc("POST /api/queue/skip", handlers.HandleSkipSong)
	mux.HandleFunc("POST /api/queue/status", handlers.HandleSetStatus)
	mux.HandleFunc("POST /api/queue/sync", handlers.HandleSyncPlayback)
	mux.HandleFunc("POST /api/queue/ended", handlers.HandleSongEnded)
	mux.HandleFunc("POST /api/queue/prev", handlers.HandlePrevSong)
	mux.HandleFunc("POST /api/queue/remove", handlers.HandleRemoveSong)
	mux.HandleFunc("POST /api/queue/clear", handlers.HandleClearQueue)
	mux.HandleFunc("POST /api/queue/volume", handlers.HandleChangeVolume)
	mux.HandleFunc("POST /api/queue/prioritize", handlers.HandlePrioritizeSong)
	mux.HandleFunc("GET /api/user/priority-balance", handlers.HandleGetPriorityBalance)
	mux.HandleFunc("GET /api/youtube/search", handlers.HandleSearchYouTube)
	mux.HandleFunc("POST /api/vote/skip", handlers.HandleVoteSkip)
	mux.HandleFunc("POST /api/vote/prioritize", handlers.HandleVotePriority)

	// Auto-queue API
	mux.HandleFunc("POST /api/autoqueue/toggle", autoQueueHandlers.HandleToggleAutoQueue)
	mux.HandleFunc("GET /api/autoqueue/status", autoQueueHandlers.HandleGetAutoQueueStatus)

	// WebSocket
	mux.HandleFunc("/ws", hub.RegisterHandler)

	return mux, cfg, nil
}

// initRepositories selects the PostgreSQL backend when cfg.DatabaseURL is
// set, otherwise the SQLite fallback. The returned *sql.DB is non-nil for
// the PostgreSQL path so the caller can close it on shutdown; it is nil for
// the SQLite path because SQLiteRepository owns its handle internally.
func initRepositories(cfg *config.Config) (
	queueRepo repository.QueueRepository,
	userRepo repository.UserRepository,
	autoQueueRepo domain.AutoQueueRepository,
	db *sql.DB,
	err error,
) {
	if cfg.DatabaseURL != "" {
		db, err = sql.Open("pgx", cfg.DatabaseURL)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if err = db.Ping(); err != nil {
			_ = db.Close()
			return nil, nil, nil, nil, err
		}

		pgQueue := persistence.NewPostgresRepository(db)
		pgUser := persistence.NewPostgresUserRepository(db)
		pgAutoQueue := persistence.NewPostgresAutoQueueRepository(db)
		return pgQueue, pgUser, pgAutoQueue, db, nil
	}

	// SQLite fallback (preserved per ADR 002 §13 / R02/R03).
	sqliteRepo, err := persistence.NewSQLiteRepository(cfg.DBPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return sqliteRepo,
		persistence.NewSQLiteUserRepository(sqliteRepo.DB()),
		persistence.NewSQLiteAutoQueueRepository(sqliteRepo.DB()),
		nil, nil
}
