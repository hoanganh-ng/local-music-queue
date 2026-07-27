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
	"time"

	delivery "local-music-queue/internal/delivery/http"
	"local-music-queue/internal/delivery/origin"
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
	usecaseRoomAutoQueue "local-music-queue/internal/usecase/roomautoqueue"
	usecaseRoomChat "local-music-queue/internal/usecase/roomchat"
	usecaseRoomQueue "local-music-queue/internal/usecase/roomqueue"
	usecaseRoomVote "local-music-queue/internal/usecase/roomvote"
	usecaseVote "local-music-queue/internal/usecase/vote"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// envMode returns the APP_ENV string value corresponding to a local flag,
// so origin.Parse sees the same value the process would otherwise have
// exposed.
func envMode(isLocal bool) string {
	if isLocal {
		return "local"
	}
	return "production"
}

// activityProducerComposition captures the three activity-producing
// interactors wired by setupApp so the same-package composition
// regression test can confirm — through their narrow
// ActivityWriterSeam accessors — that each one received the explicit
// no-op writer. It is written once during setupApp and read only by
// tests; it has no production behavior.
type activityProducerComposition struct {
	roomQueue     *usecaseRoomQueue.Interactor
	roomVote      *usecaseRoomVote.Interactor
	roomAutoQueue *usecaseRoomAutoQueue.Interactor
}

var composedActivityProducers activityProducerComposition

func main() {
	mux, cfg, policy, roomWSHub, roomVoteInteractor, cleanup, err := setupApp()
	if err != nil {
		log.Fatalf("Failed to setup application: %v", err)
	}
	defer cleanup()

	// R09b: server-owned expiry runner. Fans out RoomVoteResolved{outcome:"expired"}
	// for any room vote session that exceeded its 30s expiry window without
	// reaching a threshold. The adapter lives in cmd/server so
	// internal/usecase/roomvote stays free of any delivery/ws import.
	go expiryAdapter{
		broadcaster: roomWSHub,
		vote:        roomVoteInteractor,
		interval:    5 * time.Second,
	}.run(context.Background())

	// Start Server
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: requestLogger(enableCORS(policy, mux)),
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

func setupApp() (*http.ServeMux, *config.Config, *origin.Policy, *ws.RoomWSHub, *usecaseRoomVote.Interactor, func(), error) {
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
		return nil, nil, nil, nil, nil, nil, err
	}

	// Parse origin policy
	policy, perr := origin.Parse(map[string]string{
		"ALLOWED_ORIGINS": strings.Join(cfg.AllowedOrigins, ","),
		"APP_ENV":         envMode(cfg.IsLocal),
	})
	if perr != nil {
		return nil, nil, nil, nil, nil, nil, perr
	}
	log.Printf("Allowed origins: %v (local=%t)", policy.Allowed, policy.IsLocal)

	// 2. Initialize Infrastructure
	// PostgreSQL only: the SQLite runtime fallback was removed in R03 once the
	// data migration (cmd/migrate-data) verified successful. The backend
	// refuses to start without DATABASE_URL.
	queueRepo, userRepo, autoQueueRepo, dbHandle, err := initRepositories(cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	pgRoom := persistence.NewPostgresRoomRepository(dbHandle)
	pgRoomQueue := persistence.NewPostgresRoomQueueRepository(dbHandle)
	// roomWSHub is constructed later in the delivery wiring step (after
	// the auth interactor exists). The cleanup closure below references
	// it via a separate var so the placeholder stays typed-nil-safe until
	// the real hub is assigned.
	var roomWSHub *ws.RoomWSHub
	// dbHandle is non-nil only for the PostgreSQL path. The *sql.DB must stay
	// open for the entire server lifetime, so its Close is owned by main via
	// the cleanup closure returned below — closing it here would invalidate
	// every repository handle before ListenAndServe runs.
	cleanup := func() {
		if roomWSHub != nil {
			roomWSHub.Close()
		}
		if dbHandle != nil {
			_ = dbHandle.Close()
		}
	}

	// Run embedded PostgreSQL schema migrations defensively on backend startup.
	// The db-init Compose job is authoritative; this is belt-and-braces for
	// `go run ./cmd/server` and CI. ErrNoChange is not an error.
	if err := persistence.RunEmbeddedMigrationsUp(dbHandle); err != nil {
		cleanup()
		return nil, nil, nil, nil, nil, nil, err
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
	// R09i: the explicit no-op room-activity writer is the ONLY
	// implementation composed pre-R14c (collision guard — activities
	// are produced but not persisted until the cutover-id sprint).
	noopRoomActivityWriter := persistence.NewNoopRoomActivityRepository()
	roomQueueInteractor := usecaseRoomQueue.NewInteractor(pgRoom, pgRoomQueue, ytService, noopRoomActivityWriter)

	// R11a: per-room plain-text chat (active members only; archived
	// rooms map to 409). The interactor owns trim/CRLF normalization
	// + length validation, resolves sender display names via the
	// existing user repo, and fans the post-mutation envelope out
	// through the per-room hub via a thin adapter (so usecase/roomchat
	// stays free of delivery/ws).
	pgRoomChat := persistence.NewPostgresRoomChatMessageRepository(dbHandle)
	roomChatInteractor := usecaseRoomChat.NewInteractor(pgRoomChat, pgRoom, userRepo)
	roomQueueHandlers := delivery.NewRoomQueueHandlers(roomQueueInteractor, authInteractor)
	playerLeaseInteractor := usecaseRoom.NewPlayerLeaseInteractor(persistence.NewPostgresPlayerLeaseRepository(dbHandle), pgRoom, dbHandle, usecaseRoom.DefaultLeaseDuration, usecaseRoom.DefaultLeaseGrace)

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
	hub.SetOriginChecker(policy.AllowWebSocket)
	hub.SetAuthInteractor(authInteractor)
	go hub.Run() // Start WebSocket hub loop

	// R07b: per-room WebSocket hub. Satisfies usecase/roomqueue.Broadcaster
	// implicitly, so it can be wired straight into the room queue interactor.
	// Sequencing is single-process / in-memory; no cross-process ordering
	// claim is made. The three single-method resolvers are composed by the
	// hub constructor and delegate to the existing repositories / interactors.
	roomWSHub = ws.NewRoomWSHub(
		ws.RoomQueueResolverFunc(func(ctx context.Context, slug string) (*entity.Room, error) {
			return pgRoom.GetRoomBySlug(ctx, slug)
		}),
		ws.RoomMemberResolverFunc(func(ctx context.Context, roomID int64, userID int) (bool, error) {
			if _, err := pgRoom.GetMember(ctx, roomID, userID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		}),
		ws.RoomQueueStateResolverFunc(func(ctx context.Context, roomID int64) (*entity.Queue, error) {
			return roomQueueInteractor.GetStateByRoomID(ctx, roomID)
		}),
	)
	roomWSHub.SetOriginChecker(policy.AllowWebSocket)
	roomWSHub.SetSessionResolver(authInteractor)
	go roomWSHub.Run()

	// Wire the room queue interactor's broadcaster seam to the per-room WS
	// hub. nil-safe: the seam tolerates an unset broadcaster; the handler
	// path short-circuits without a broadcast when nil.
	roomQueueInteractor.SetBroadcaster(roomWSHub)

	// R10b: wire the per-room members broadcaster seam so RemoveMemberByHost
	// can fan out the targeted room_member_removed envelope and the
	// remaining-clients room_members_changed envelope without usecase/room
	// importing delivery/ws. The adapter below satisfies the seam using
	// the existing per-room hub methods (BroadcastRoomMemberRemoved,
	// BroadcastRoomMembersChanged, CloseRemovedClient).
	roomMembersAdapter := roomMembersBroadcasterAdapter{hub: roomWSHub}
	roomInteractor.SetMembersBroadcaster(roomMembersAdapter)

	// R11a: wire the per-room chat broadcaster seam so the roomchat
	// interactor can fan out the post-mutation room_chat_message_created
	// envelope after a successful persistence without usecase/roomchat
	// importing delivery/ws. The adapter below resolves the sender
	// display name via the user repo (with the "user #<id>" fallback
	// per 016-room-chat-feature.md) and forwards the canonical tuple
	// (roomSlug, *entity.RoomChatMessage, displayName) to the per-room
	// hub's BroadcastRoomChatMessageCreated method.
	roomChatBroadcasterAdapter := roomChatBroadcasterAdapter{hub: roomWSHub}
	roomChatInteractor.SetBroadcaster(roomChatBroadcasterAdapter)

	// R09a: wire the lease-authorizer seam so direct playback mutations
	// (status / sync / skip / ended) enforce the player-lease holder rule
	// at the use-case layer. PlayerLeaseInteractor implements
	// room.PlaybackLeaseAuthorizer directly.
	roomQueueInteractor.SetLeaseAuthorizer(playerLeaseInteractor)

	// R09f: per-room auto-queue runtime. The new use case
	// orchestrates config read + recommendation fetch + stale
	// insertion + broadcast. It does NOT touch the global
	// auto-queue tables or the global /ws event; it rides
	// /ws/rooms/{slug} only.
	pgRoomAutoQueue := persistence.NewPostgresRoomAutoQueueRepository(dbHandle)
	roomAutoQueueInteractor := usecaseRoomAutoQueue.NewInteractor(pgRoom, pgRoomAutoQueue, ytRelatedFetcher, noopRoomActivityWriter)
	roomAutoQueueInteractor.SetQueueSnapshotLoader(roomQueueInteractor.GetStateByRoomID)
	roomAutoQueueInteractor.SetAddRoomAutoQueueSongFunc(func(ctx context.Context, slug string, song *entity.Song, expectedSourceSongID string) (*usecaseRoomAutoQueue.AddRoomAutoQueueSongResult, error) {
		q, ci, cs, st, el, err := roomQueueInteractor.AddRoomAutoQueueSong(ctx, slug, song, expectedSourceSongID)
		if err != nil {
			if errors.Is(err, usecaseRoomQueue.ErrRoomAutoQueueStale) {
				return nil, usecaseRoomAutoQueue.ErrAutoQueueStale
			}
			return nil, err
		}
		return &usecaseRoomAutoQueue.AddRoomAutoQueueSongResult{
			Queue: q, CurrentIndex: ci, CurrentSong: cs, Status: st, Elapsed: el,
		}, nil
	})
	// Broadcast adapter: the roomautoqueue use case exposes a typed
	// narrow broadcaster seam (RoomAutoQueueBroadcaster) — the per-room
	// hub satisfies it directly via its BroadcastRoomAutoQueueAdded
	// method, so no wrapping is needed. The adapter passes the full
	// (room_slug, song, source_song_title, current_index, current_song,
	// status, elapsed, state) tuple returned by roomqueue.AddRoomAutoQueueSong
	// to the hub, which builds the canonical RoomAutoQueueAddedData
	// envelope inside dispatch().
	roomAutoQueueInteractor.SetBroadcaster(roomWSHub)
	// Hand the roomautoqueue use case to the roomqueue interactor so
	// successful mutations that leave current==last fire it as a
	// non-blocking goroutine. Nil-safe: the trigger short-circuits
	// when unset.
	roomQueueInteractor.SetRoomAutoQueueTrigger(roomAutoQueueInteractor)

	// R09b: wire the roomvote interactor AFTER the real *ws.RoomWSHub is
	// assigned. The vote interactor does NOT receive a broadcaster seam
	// — it returns Outcome structs that the HTTP handler fans out via
	// the existing roomqueue.Broadcaster (which is wired to the per-room
	// WS hub). The Resolver is the per-room hub's
	// UniqueConnectedUserIDs(slug) so the threshold is captured from
	// the live connection set at session creation. 30s expiry mirrors
	// the global vote package.
	roomVoteInteractor := usecaseRoomVote.NewInteractor(roomQueueInteractor, roomWSHub, 30*time.Second, noopRoomActivityWriter)
	roomVoteHandlers := delivery.NewRoomVoteHandlers(roomVoteInteractor, roomQueueInteractor, authInteractor)

	// R09i composition seam: capture the three activity-producing
	// interactors so the same-package composition regression test can
	// verify (via their ActivityWriterSeam accessors) that normal
	// setupApp wiring selects the explicit no-op writer and never the
	// PostgreSQL repository. Test-read only; carries no runtime role.
	composedActivityProducers = activityProducerComposition{
		roomQueue:     roomQueueInteractor,
		roomVote:      roomVoteInteractor,
		roomAutoQueue: roomAutoQueueInteractor,
	}

	// Wire auto-queue broadcaster to WS hub
	autoQueueInteractor.SetBroadcaster(hub.Broadcast)

	// Wire vote interactor into hub
	hub.SetVoteInteractor(voteInteractor)

	// Wire priority interactor into hub
	hub.SetPriorityInteractor(priorityInteractor)

	// Wire player-lease sweeper into hub. The adapter converts the
	// usecase/room event shape into the ws package's local shape so ws
	// does not import usecase.
	hub.SetPlayerLeaseSweeper(wsLeaseSweeperAdapter{interactor: playerLeaseInteractor})

	handlers := delivery.NewHandlers(qInteractor, authInteractor, actInteractor, priorityInteractor, voteInteractor, hub)
	autoQueueHandlers := delivery.NewAutoQueueHandlers(autoQueueInteractor, hub)
	roomHandlers := delivery.NewRoomHandlers(roomInteractor, playerLeaseInteractor, authInteractor)
	roomChatHandlers := delivery.NewRoomChatHandlers(roomChatInteractor)
	// Adapter lets the room handler publish room_archived without
	// importing the ws package. hub.Broadcast is non-blocking when called
	// from outside Hub.Run; for the in-ticker case the goroutine fan-out
	// added in hub.go keeps the hub loop from deadlocking itself.
	roomHandlers.SetArchiveBroadcaster(hubArchiveBroadcaster{hub: hub})

	// R10b: the per-room archive broadcast on host archive, plus the
	// targeted room_member_removed + remaining-clients room_members_changed
	// broadcasts on member removal, all ride the per-room hub. The
	// adapter above is the production seam.
	roomHandlers.SetMemberBroadcaster(roomMembersAdapter)

	// 5. Setup Routes
	mux := http.NewServeMux()

	// R05 — privileged REST routes use the shared auth middleware so identity
	// is server-resolved from the bearer token. Role gates are applied to
	// endpoints that are restricted to host/admin or to guest/admin.
	//
	// Role policy:
	//   - Playback control (skip / status / sync / ended / prev / volume /
	//     clear) is host/admin only.
	//   - Auto-queue toggle is host/admin only.
	//   - Voting (skip / prioritize) is guest/admin only.
	//   - Add song, prioritize, and remove accept any authenticated user
	//     (server-resolved identity is used for attribution / ownership).
	auth := delivery.RequireAuth(authInteractor)
	hostOrAdmin := delivery.RequireRole(entity.RoleHost, entity.RoleAdmin)
	voterOrAdmin := delivery.RequireRole(entity.RoleGuest, entity.RoleAdmin)

	// HTTP API
	mux.HandleFunc("POST /api/auth/google", handlers.HandleGoogleLogin)
	mux.HandleFunc("POST /api/auth", handlers.HandleLogin) // Deprecated
	mux.HandleFunc("GET /api/queue", handlers.HandleGetQueue)
	mux.HandleFunc("POST /api/queue/add", auth(handlers.HandleAddSong))
	mux.HandleFunc("POST /api/queue/skip", auth(hostOrAdmin(handlers.HandleSkipSong)))
	mux.HandleFunc("POST /api/queue/status", auth(hostOrAdmin(handlers.HandleSetStatus)))
	mux.HandleFunc("POST /api/queue/sync", auth(hostOrAdmin(handlers.HandleSyncPlayback)))
	mux.HandleFunc("POST /api/queue/ended", auth(hostOrAdmin(handlers.HandleSongEnded)))
	mux.HandleFunc("POST /api/queue/prev", auth(hostOrAdmin(handlers.HandlePrevSong)))
	mux.HandleFunc("POST /api/queue/remove", auth(handlers.HandleRemoveSong))
	mux.HandleFunc("POST /api/queue/clear", auth(hostOrAdmin(handlers.HandleClearQueue)))
	mux.HandleFunc("POST /api/queue/volume", auth(hostOrAdmin(handlers.HandleChangeVolume)))
	mux.HandleFunc("POST /api/queue/prioritize", auth(handlers.HandlePrioritizeSong))
	mux.HandleFunc("GET /api/user/priority-balance", handlers.HandleGetPriorityBalance)
	mux.HandleFunc("GET /api/youtube/search", handlers.HandleSearchYouTube)
	mux.HandleFunc("POST /api/vote/skip", auth(voterOrAdmin(handlers.HandleVoteSkip)))
	mux.HandleFunc("POST /api/vote/prioritize", auth(voterOrAdmin(handlers.HandleVotePriority)))

	// Auto-queue API
	mux.HandleFunc("POST /api/autoqueue/toggle", auth(hostOrAdmin(autoQueueHandlers.HandleToggleAutoQueue)))
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
	// R10b: DELETE /api/rooms/{slug} — host-requested soft archive.
	mux.HandleFunc("DELETE /api/rooms/{slug}", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleDeleteRoom(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	// R10b: DELETE /api/rooms/{slug}/members/{userId} — host-driven
	// member removal. userId is parsed in main.go so a non-integer or
	// <= 0 path value returns 400 with the R10a JSON body shape
	// `{"error": "invalid user id"}`.
	mux.HandleFunc("DELETE /api/rooms/{slug}/members/{userId}", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		userID, perr := strconv.Atoi(r.PathValue("userId"))
		if perr != nil || userID <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid user id"}` + "\n"))
			return
		}
		roomHandlers.HandleDeleteMember(w, r, r.PathValue("slug"), actorFromCtx(r.Context()), userID)
	}))
	mux.HandleFunc("POST /api/invites/{token}/redeem", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleRedeemInvite(w, r, r.PathValue("token"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/player/claim", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleClaimPlayer(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/player/heartbeat", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleHeartbeatPlayer(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/player/release", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleReleasePlayer(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("GET /api/rooms/{slug}/player/lease", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomHandlers.HandleGetPlayerLease(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("GET /api/rooms/{slug}/queue", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleGetRoomQueue(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/queue/add", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleAddRoomSong(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/queue/remove", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleRemoveRoomSong(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/queue/clear", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleClearRoomQueue(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/queue/prioritize", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandlePrioritizeRoomSong(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	// R09a: lease-aware direct playback controls. All four routes sit
	// behind roomAuth (bearer token) and additionally enforce
	// player-lease holder authorization inside roomqueue.Interactor
	// (not just at the handler boundary). The matching per-room
	// WebSocket events (room_playback_*) ride /ws/rooms/{slug} only;
	// the global /ws 16-event inventory is unchanged.
	//
	// R09b: room vote-to-skip. POST /api/rooms/{slug}/vote/skip sits
	// behind roomAuth (bearer token) and is restricted to active room
	// members (any role); it BYPASSES the player-lease holder rule by
	// design. The vote session state lives in a new
	// internal/usecase/roomvote package: in-memory, room-scoped,
	// current-song-scoped, 30-second expiry, single-instance only,
	// never persisted. Threshold is captured at session creation via
	// (*RoomWSHub).UniqueConnectedUserIDs(slug), which de-duplicates
	// multi-tab connections by userID. On a passed vote the queue
	// advances to the next song via the existing roomqueue mutex and
	// the matching per-room WebSocket events (room_vote_updated +
	// room_vote_resolved + room_playback_song_advanced with reason
	// "skip") ride /ws/rooms/{slug} only. The global /ws 16-event
	// inventory, global /api/vote/..., global /api/queue/...,
	// priority balances, and auto-queue are all unchanged.
	mux.HandleFunc("POST /api/rooms/{slug}/playback/status", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleSetRoomPlaybackStatus(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/playback/sync", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleSyncRoomPlayback(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/playback/skip", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleSkipRoomPlayback(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/playback/ended", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleRoomSongEnded(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/playback/volume", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleChangeRoomPlaybackVolume(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	// R09d: room-scoped previous playback command. Lease-holder only.
	// Moves the room queue from the current song to the previous song
	// and broadcasts room_playback_song_previous on /ws/rooms/{slug}.
	// Returns 400 when the queue has no current song or is already on
	// the first song; the queue is NOT mutated, saved, or broadcast on
	// those error paths. The global /ws 16-event inventory and the
	// global /api/queue/prev contract are unchanged.
	mux.HandleFunc("POST /api/rooms/{slug}/playback/prev", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomQueueHandlers.HandleChangeRoomPlaybackPrevious(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/vote/skip", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomVoteHandlers.HandleCastRoomVoteSkip(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	// R09h: room vote-to-prioritize. POST /api/rooms/{slug}/vote/prioritize
	// sits behind roomAuth (bearer token) and is restricted to active room
	// members (any role); like vote-to-skip it BYPASSES the player-lease
	// rule and requires no room/global role. The body is exactly
	// {"song_index": <integer>} with strict decoding (missing, negative,
	// malformed, trailing, and unknown fields are rejected). Prioritize
	// vote sessions share the roomvote in-memory map (keyed
	// prioritize:{slug}:{songID}), 30-second expiry, single-instance only;
	// expiry is handled by the existing shared sweep, not a second loop.
	// On a passed vote the target song is moved immediately after the
	// current song via the existing roomqueue mutex and the matching
	// per-room WebSocket events (room_vote_updated + room_vote_resolved +
	// room_queue_song_prioritized) ride /ws/rooms/{slug} only. The global
	// /ws inventory, global /api/vote/..., global /api/queue/..., priority
	// balances, and auto-queue are all unchanged.
	mux.HandleFunc("POST /api/rooms/{slug}/vote/prioritize", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomVoteHandlers.HandleCastRoomVotePrioritize(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	// R09f: per-room auto-queue endpoints.
	//   GET   /api/rooms/{slug}/autoqueue/status  — any active member.
	//   POST  /api/rooms/{slug}/autoqueue/toggle  — host/admin only.
	// Both routes sit behind roomAuth (bearer token) so actor
	// identity is server-resolved. The toggle handler enforces
	// host/admin role inside its own layer. The R09f broadcasts
	// ride /ws/rooms/{slug} only; the global /ws 16-event
	// inventory is unchanged.
	roomAutoQueueHandlers := delivery.NewRoomAutoQueueHandlers(roomAutoQueueInteractor, authInteractor, roomWSHub)
	mux.HandleFunc("GET /api/rooms/{slug}/autoqueue/status", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomAutoQueueHandlers.HandleGetRoomAutoQueueStatus(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/autoqueue/toggle", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomAutoQueueHandlers.HandleRoomAutoQueueToggle(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	// R11a: per-room plain-text chat. Both routes sit behind roomAuth
	// (bearer token) so actor identity is server-resolved from the
	// session; the request body never carries a sender_id. The
	// GET response and POST 201 body share the same per-message
	// wire shape (`{id, room_slug, sender:{user_id, display_name},
	// content, created_at}`) so the frontend can route incoming
	// WS events through the same applyRoomChatMessageCreated store
	// mutator as the initial REST seed. Active-membership is
	// enforced by the chat interactor; archived rooms map to 409.
	// No retention purge in R11a; lifecycle hardening is deferred
	// to R10f+.
	mux.HandleFunc("GET /api/rooms/{slug}/chat/messages", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomChatHandlers.HandleListChatMessages(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))
	mux.HandleFunc("POST /api/rooms/{slug}/chat/messages", roomAuth(func(w http.ResponseWriter, r *http.Request) {
		roomChatHandlers.HandlePostChatMessage(w, r, r.PathValue("slug"), actorFromCtx(r.Context()))
	}))

	// WebSocket
	mux.HandleFunc("/ws", hub.RegisterHandler)
	// R07b: per-room WebSocket endpoint. Shares the same ALLOWED_ORIGINS
	// policy as the global /ws endpoint through roomWSHub.originChecker.
	mux.HandleFunc("/ws/rooms/{slug}", roomWSHub.RegisterHandler)

	return mux, cfg, policy, roomWSHub, roomVoteInteractor, cleanup, nil
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

// wsLeaseSweeperAdapter converts usecase/room.RoomArchivedEvent slices
// returned by PlayerLeaseInteractor.SweepExpired into the ws package's
// RoomArchivedBroadcast shape so the ws package does not import usecase.
type wsLeaseSweeperAdapter struct {
	interactor *usecaseRoom.PlayerLeaseInteractor
}

// SweepExpired implements ws.PlayerLeaseSweeper.
func (a wsLeaseSweeperAdapter) SweepExpired(ctx context.Context) []ws.RoomArchivedBroadcast {
	src := a.interactor.SweepExpired(ctx)
	out := make([]ws.RoomArchivedBroadcast, len(src))
	for i, e := range src {
		out[i] = ws.RoomArchivedBroadcast{
			RoomID:     e.RoomID,
			Reason:     e.Reason,
			ArchivedAt: e.ArchivedAt,
		}
	}
	return out
}

// hubArchiveBroadcaster adapts *ws.Hub to the room.RoomArchivedBroadcaster
// interface so usecase/room stays free of delivery/ws imports.
type hubArchiveBroadcaster struct {
	hub *ws.Hub
}

// BroadcastRoomArchived implements room.RoomArchivedBroadcaster.
func (b hubArchiveBroadcaster) BroadcastRoomArchived(ev usecaseRoom.RoomArchivedEvent) {
	b.hub.Broadcast(ws.EventRoomArchived, ws.RoomArchivedData{
		RoomID:     ev.RoomID,
		Reason:     ev.Reason,
		ArchivedAt: ev.ArchivedAt,
	})
}

// roomMembersBroadcasterAdapter adapts *ws.RoomWSHub to the
// room.RoomMembersBroadcaster interface so usecase/room stays free of
// delivery/ws imports. R10b addition.
type roomMembersBroadcasterAdapter struct {
	hub *ws.RoomWSHub
}

// BroadcastRoomArchived implements room.RoomMembersBroadcaster.
// Stamps the per-room room_archived envelope with the given reason
// and time.Now(); the existing R06 envelope shape is reused.
func (a roomMembersBroadcasterAdapter) BroadcastRoomArchived(roomSlug string, reason string) {
	a.hub.BroadcastRoomArchived(roomSlug, reason, time.Now())
}

// BroadcastRoomMemberRemoved implements room.RoomMembersBroadcaster.
// The per-room hub's BroadcastRoomMemberRemoved delivers the
// targeted envelope to the target user's connections; the close
// frame is sent separately by CloseRemovedClient.
func (a roomMembersBroadcasterAdapter) BroadcastRoomMemberRemoved(roomSlug string, targetUserID int, reason string) {
	a.hub.BroadcastRoomMemberRemoved(roomSlug, targetUserID, reason)
}

// BroadcastRoomMembersChanged implements room.RoomMembersBroadcaster.
// The per-room hub's BroadcastRoomMembersChanged fans out to
// remaining clients, excluding the target user whose connections
// are about to be closed. The seam carries excludeUserID through
// to the hub so the production adapter honors the targeted
// fan-out — without this, the room_members_changed envelope would
// race the close-frame path and potentially reach the removed user.
func (a roomMembersBroadcasterAdapter) BroadcastRoomMembersChanged(roomSlug string, members []entity.RoomMember, excludeUserID int) {
	if len(members) == 0 {
		return
	}
	a.hub.BroadcastRoomMembersChanged(roomSlug, members, excludeUserID)
}

// CloseRemovedClient implements room.RoomMembersBroadcaster. Sends
// a close frame with code 1008 to the target user's per-room
// connections and unregisters them from the hub.
func (a roomMembersBroadcasterAdapter) CloseRemovedClient(roomSlug string, targetUserID int) {
	a.hub.CloseRemovedClient(roomSlug, targetUserID)
}

// roomChatBroadcasterAdapter adapts *ws.RoomWSHub to the
// roomchat.Broadcaster interface so usecase/roomchat stays free of
// delivery/ws imports. R11a addition.
//
// The displayName argument is the resolved sender display name
// (user.DisplayName, or the documented "user #<id>" fallback when
// the row's display_name is empty). The interactor owns the
// resolution so the broadcaster stays free of any user-repo
// dependency and the wire envelope stays consistent with the REST
// response shape.
type roomChatBroadcasterAdapter struct {
	hub *ws.RoomWSHub
}

// BroadcastRoomChatMessageCreated implements roomchat.Broadcaster.
// R11a addition; rides /ws/rooms/{slug} only. The global /ws
// 16-event inventory is unchanged.
func (a roomChatBroadcasterAdapter) BroadcastRoomChatMessageCreated(roomSlug string, msg *entity.RoomChatMessage, displayName string) {
	if msg == nil {
		return
	}
	a.hub.BroadcastRoomChatMessageCreated(roomSlug, msg, displayName)
}

// expiryAdapter is the server-owned seam that periodically calls
// (*roomvote.Interactor).ExpireSessions and broadcasts the resulting
// resolutions on /ws/rooms/{slug}. It is defined here (cmd/server)
// so internal/usecase/roomvote stays free of any delivery/ws import.
// The interface contract is a subset of roomvote.Interactor; we
// accept an interface so tests can inject a stub without booting the
// real interactor.
type expiryAdapter struct {
	broadcaster interface {
		BroadcastRoomVoteResolved(slug, sessionID, outcome string, state *entity.Queue)
	}
	vote     votingExpireRunner
	interval time.Duration
}

// runOnce performs a single expiry sweep and fans out the resolved
// sessions. Failure is logged and ignored — expiry is best-effort.
func (a expiryAdapter) runOnce(ctx context.Context) {
	out, err := a.vote.ExpireSessions(ctx)
	if err != nil {
		log.Printf("roomvote: ExpireSessions failed: %v", err)
		return
	}
	for _, e := range out {
		if e.Session == nil {
			continue
		}
		a.broadcaster.BroadcastRoomVoteResolved(e.RoomSlug, e.SessionID, "expired", e.StateQueue)
	}
}

// run blocks on the supplied ticker, calling runOnce at every tick
// until ctx is cancelled. The ticker interval defaults to 5 seconds
// when zero, which is a defensive bounds for the 30-second expiry
// window on room vote sessions.
func (a expiryAdapter) run(ctx context.Context) {
	interval := a.interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.runOnce(ctx)
		}
	}
}

// votingExpireRunner is the subset of *roomvote.Interactor that the
// adapter needs. Declared here so the adapter test can inject a
// fake without booting the real interactor or depending on the
// internal concrete type beyond its public signature.
type votingExpireRunner interface {
	ExpireSessions(ctx context.Context) ([]usecaseRoomVote.ExpiredOutcome, error)
}
