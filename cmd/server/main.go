package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	delivery "local-music-queue/internal/delivery/http"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/infrastructure/config"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/infrastructure/youtube"
	usecaseActivity "local-music-queue/internal/usecase/activity"
	usecaseAuth "local-music-queue/internal/usecase/auth"
	usecasePriority "local-music-queue/internal/usecase/priority"
	usecaseQueue "local-music-queue/internal/usecase/queue"
	usecaseVote "local-music-queue/internal/usecase/vote"
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
	repo, err := persistence.NewSQLiteRepository(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}

	// Initialize user repository
	userRepo := persistence.NewSQLiteUserRepository(repo.DB())

	ytService := youtube.NewYTDLPService(cfg.YTDLPPath)

	// 3. Initialize Usecases
	qInteractor := usecaseQueue.NewInteractor(repo, ytService)
	authInteractor := usecaseAuth.NewInteractor(userRepo, googleClientID, hostEmails, adminEmails)
	actInteractor := usecaseActivity.NewInteractor(repo)
	priorityInteractor := usecasePriority.NewInteractor(userRepo, repo)
	voteInteractor := usecaseVote.NewInteractor(repo, userRepo, 0) // 0 = default 30s expiry

	// 4. Initialize Delivery with queue state callback
	hub := ws.NewHub(qInteractor.GetState)
	go hub.Run() // Start WebSocket hub loop

	// Wire vote interactor into hub
	hub.SetVoteInteractor(voteInteractor)

	// Wire priority interactor into hub
	hub.SetPriorityInteractor(priorityInteractor)

	handlers := delivery.NewHandlers(qInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub)

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

	// WebSocket
	mux.HandleFunc("/ws", hub.RegisterHandler)

	return mux, cfg, nil
}
