package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
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
	usecaseRoom "local-music-queue/internal/usecase/room"
	usecaseVote "local-music-queue/internal/usecase/vote"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	mux, cfg, cleanup, err := setupApp()
	if err != nil {
		log.Fatalf("Failed to setup application: %v", err)
	}
	defer cleanup()

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

func setupApp() (*http.ServeMux, *config.Config, func(), error) {
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
		return nil, nil, nil, err
	}

	// 2. Initialize Infrastructure
	// PostgreSQL only: the SQLite runtime fallback was removed in R03 once the
	// data migration (cmd/migrate-data) verified successful. The backend
	// refuses to start without DATABASE_URL.
	queueRepo, userRepo, autoQueueRepo, dbHandle, err := initRepositories(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	pgRoom := persistence.NewPostgresRoomRepository(dbHandle)
	// dbHandle is non-nil only for the PostgreSQL path. The *sql.DB must stay
	// open for the entire server lifetime, so its Close is owned by main via
	// the cleanup closure returned below — closing it here would invalidate
	// every repository handle before ListenAndServe runs.
	cleanup := func() {
		if dbHandle != nil {
			_ = dbHandle.Close()
		}
	}

	// Run embedded PostgreSQL schema migrations defensively on backend startup.
	// The db-init Compose job is authoritative; this is belt-and-braces for
	// `go run ./cmd/server` and CI. ErrNoChange is not an error.
	if err := persistence.RunEmbeddedMigrationsUp(dbHandle); err != nil {
		cleanup()
		return nil, nil, nil, err
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
	roomInteractor := usecaseRoom.NewInteractor(pgRoom)

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
	roomHandlers := delivery.NewRoomHandlers(roomInteractor, authInteractor)

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

	// Room API (R04) — all routes require a valid bearer token; the resolved
	// actor user id is injected into the request context by roomAuth.
	roomAuth := makeRoomActor(authInteractor)
	mux.HandleFunc("POST /api/rooms", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleCreateRoom(w, r, actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("GET /api/rooms", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleListRooms(w, r, actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("GET /api/rooms/{slug}", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleGetRoom(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("GET /api/rooms/{slug}/members", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleListMembers(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/members/{userId}/promote", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		userID, err := strconv.Atoi(r.PathValue("userId"))
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		roomHandlers.HandlePromoteMember(w, r, r.PathValue("slug"), actorFromCtx(r.Context()), userID)
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/members/{userId}/demote", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		userID, err := strconv.Atoi(r.PathValue("userId"))
		if err != nil {
			http.Error(w, "invalid user id", http.StatusBadRequest)
			return
		}
		roomHandlers.HandleDemoteMember(w, r, r.PathValue("slug"), actorFromCtx(r.Context()), userID)
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/invites", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleCreateInvite(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("GET /api/rooms/{slug}/invites", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleListInvites(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("DELETE /api/rooms/{slug}/invites/{inviteId}", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		inviteID, err := strconv.ParseInt(r.PathValue("inviteId"), 10, 64)
		if err != nil {
			http.Error(w, "invalid invite id", http.StatusBadRequest)
			return
		}
		roomHandlers.HandleRevokeInvite(w, r, r.PathValue("slug"), inviteID, actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/invites/{token}/redeem", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleRedeemInvite(w, r, r.PathValue("token"), actorFromCtx(r.Context()))
	}))

	// WebSocket
	mux.HandleFunc("/ws", hub.RegisterHandler)

	return mux, cfg, cleanup, nil
}

// initRepositories opens the PostgreSQL backend. The R03 migration removed
// the SQLite runtime fallback; this function now fails fast when no
// DATABASE_URL is configured so a misconfigured deploy cannot accidentally
// start with no persistence at all.
//
// The returned *sql.DB is non-nil so the caller can close it on shutdown.
func initRepositories(cfg *config.Config) (
	queueRepo repository.QueueRepository,
	userRepo repository.UserRepository,
	autoQueueRepo domain.AutoQueueRepository,
	db *sql.DB,
	err error,
) {
	if cfg.DatabaseURL == "" {
		return nil, nil, nil, nil, errors.New("DATABASE_URL (or POSTGRES_HOST/_USER/_PASSWORD/_DB) is required: SQLite fallback was removed in R03")
	}
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

// makeRoomActor returns a middleware that resolves the bearer token via the
// auth interactor, rejects unauthenticated callers with 401, and injects the
// resolved actor user id into the request context so downstream handlers can
// read it via actorFromCtx. The auth interactor is captured in a closure so
// the wrapper is bound to the same instance configured for the rest of the
// app.
func makeRoomActor(a *usecaseAuth.Interactor) func(http.HandlerFunc) http.HandlerFunc {
	return func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := delivery.ExtractToken(r)
			if token == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			user, err := a.ResolveSession(r.Context(), token)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), actorKey{}, user.ID)
			fn(w, r.WithContext(ctx))
		}
	}
}

// actorKey is the private context key under which makeRoomActor stores the
// resolved actor user id. It is intentionally unexported and unique to this
// package so it cannot collide with keys defined elsewhere.
type actorKey struct{}

// actorFromCtx extracts the actor user id stashed in the request context by
// makeRoomActor. Returns 0 when the context was not produced by the wrapper
// (which should not happen for room routes but is a safe default).
func actorFromCtx(ctx context.Context) int {
	v, _ := ctx.Value(actorKey{}).(int)
	return v
}
