package main

import (
	"log"
	"net/http"

	delivery "local-music-queue/internal/delivery/http"
	"local-music-queue/internal/delivery/ws"
	"local-music-queue/internal/infrastructure/config"
	"local-music-queue/internal/infrastructure/persistence"
	"local-music-queue/internal/infrastructure/youtube"
	usecaseActivity "local-music-queue/internal/usecase/activity"
	usecaseAuth "local-music-queue/internal/usecase/auth"
	usecaseQueue "local-music-queue/internal/usecase/queue"
)

func main() {
	mux, cfg, err := setupApp()
	if err != nil {
		log.Fatalf("Failed to setup application: %v", err)
	}

	// Start Server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: enableCORS(mux),
	}

	log.Printf("Server listening on http://localhost:%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func setupApp() (*http.ServeMux, *config.Config, error) {
	// 1. Load configuration
	cfg := config.Load()
	log.Printf("Client PIN: %s", cfg.ClientPIN)
	log.Printf("Host PIN: %s", cfg.HostPIN)
	log.Printf("Starting Local Music Queue server on port %s", cfg.Port)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}

	// 2. Initialize Infrastructure
	repo, err := persistence.NewSQLiteRepository(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}

	ytService := youtube.NewYTDLPService(cfg.YTDLPPath)

	// 3. Initialize Usecases
	qInteractor := usecaseQueue.NewInteractor(repo, ytService)
	authInteractor := usecaseAuth.NewInteractor(cfg.ClientPIN, cfg.HostPIN)
	actInteractor := usecaseActivity.NewInteractor(repo)

	// 4. Initialize Delivery with queue state callback
	hub := ws.NewHub(qInteractor.GetState)
	go hub.Run() // Start WebSocket hub loop

	handlers := delivery.NewHandlers(qInteractor, authInteractor, actInteractor, hub)

	// 5. Setup Routes
	mux := http.NewServeMux()

	// HTTP API
	mux.HandleFunc("POST /api/auth", handlers.HandleLogin)
	mux.HandleFunc("GET /api/queue", handlers.HandleGetQueue)
	mux.HandleFunc("POST /api/queue/add", handlers.HandleAddSong)
	mux.HandleFunc("POST /api/queue/skip", handlers.HandleSkipSong)
	mux.HandleFunc("POST /api/queue/status", handlers.HandleSetStatus)
	mux.HandleFunc("POST /api/queue/sync", handlers.HandleSyncPlayback)
	mux.HandleFunc("POST /api/queue/ended", handlers.HandleSongEnded)
	mux.HandleFunc("POST /api/queue/prev", handlers.HandlePrevSong)
	mux.HandleFunc("POST /api/queue/remove", handlers.HandleRemoveSong)
	mux.HandleFunc("POST /api/queue/clear", handlers.HandleClearQueue)
	mux.HandleFunc("GET /api/youtube/search", handlers.HandleSearchYouTube)

	// WebSocket
	mux.HandleFunc("/ws", hub.RegisterHandler)

	return mux, cfg, nil
}

// enableCORS is a middleware that adds CORS headers to the response
func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow any origin for development
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
