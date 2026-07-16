import { sessionHelper } from './session'

const API_BASE = import.meta.env.VITE_API_BASE_URL || 'https://localhost:443'

export class APIError extends Error {
  constructor(status, message) {
    super(message)
    this.status = status
    this.name = 'APIError'
  }
}

export const api = {
  async request(endpoint, options = {}) {
    const url = `${API_BASE}/api${endpoint}`

    const defaultHeaders = {
      'Content-Type': 'application/json'
    }

    if (sessionHelper.isValid()) {
      defaultHeaders['Authorization'] = `Bearer ${sessionHelper.getToken()}`
    }

    const config = {
      ...options,
      headers: {
        ...defaultHeaders,
        ...options.headers
      }
    }

    if (config.body && typeof config.body === 'object') {
      config.body = JSON.stringify(config.body)
    }

    const response = await fetch(url, config)

    // 204 No Content has no JSON body
    if (response.status === 204) {
      return null
    }

    if (!response.ok) {
      const errorText = await response.text()
      throw new APIError(response.status, errorText || `API Request failed with status ${response.status}`)
    }

    return await response.json()
  },

  // Auth
  async login(pin, displayName) {
    return this.request('/auth', {
      method: 'POST',
      body: { pin, display_name: displayName }
    })
  },

  async loginWithGoogle(idToken) {
    return this.request('/auth/google', {
      method: 'POST',
      body: { id_token: idToken }
    })
  },

  // Queue Operations
  async getQueue() {
    return this.request('/queue', {
      method: 'GET'
    })
  },

  async addSong(url, addedBy, addedByID, metadata = null) {
    const body = { url, added_by: addedBy, added_by_id: addedByID }
    if (metadata) {
      body.metadata = metadata
    }
    return this.request('/queue/add', {
      method: 'POST',
      body
    })
  },

  async skipSong(requestedBy) {
    return this.request('/queue/skip', {
      method: 'POST',
      body: { requested_by: requestedBy }
    })
  },

  async setStatus(status, requestedBy) {
    return this.request('/queue/status', {
      method: 'POST',
      body: { status, requested_by: requestedBy }
    })
  },

  async syncPlayback(elapsed) {
    return this.request('/queue/sync', {
      method: 'POST',
      body: { elapsed }
    })
  },

  async songEnded() {
    return this.request('/queue/ended', {
      method: 'POST'
    })
  },

  async prevSong(requestedBy) {
    return this.request('/queue/prev', {
      method: 'POST',
      body: { requested_by: requestedBy }
    })
  },

  async removeSong(index, requestedBy) {
    return this.request('/queue/remove', {
      method: 'POST',
      body: { index, requested_by: requestedBy }
    })
  },

  async clearQueue(requestedBy) {
    return this.request('/queue/clear', {
      method: 'POST',
      body: { requested_by: requestedBy }
    })
  },

  async changeVolume(direction) {
    return this.request('/queue/volume', {
      method: 'POST',
      body: { direction }
    })
  },

  // YouTube Search
  async searchYouTube(query) {
    return this.request(`/youtube/search?q=${encodeURIComponent(query)}`, {
      method: 'GET'
    })
  },

  // Priority
  async prioritizeSong(userID, songIndex) {
    return this.request('/queue/prioritize', {
      method: 'POST',
      body: { user_id: userID, song_index: songIndex }
    })
  },

  async getPriorityBalance(userID) {
    return this.request(`/user/priority-balance?user_id=${userID}`, {
      method: 'GET'
    })
  },

  // Voting
  async castSkipVote(userID, userRole) {
    return this.request('/vote/skip', {
      method: 'POST',
      body: { user_id: userID, user_role: userRole }
    })
  },

  async castPriorityVote(userID, userRole, songIndex) {
    return this.request('/vote/prioritize', {
      method: 'POST',
      body: { user_id: userID, user_role: userRole, song_index: songIndex }
    })
  },

  // Auto-queue
  async getAutoQueueStatus() {
    return this.request('/autoqueue/status', {
      method: 'GET'
    })
  },

  async setAutoQueueEnabled(enabled) {
    return this.request('/autoqueue/toggle', {
      method: 'POST',
      body: { enabled }
    })
  },

  // R09g: per-room auto-queue REST endpoints. R09f ships the
  // backend; the frontend exposes a thin read + a host/admin toggle
  // (host/admin is enforced by the backend — frontend does NOT
  // pre-check the role). Status is open to any active member; toggle
  // is host/admin only. Body shape is exactly { enabled } — no
  // strategy, no identity fields.
  async getRoomAutoQueueStatus(slug) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/autoqueue/status`, {
      method: 'GET'
    })
  },

  async setRoomAutoQueueEnabled(slug, enabled) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/autoqueue/toggle`, {
      method: 'POST',
      body: { enabled }
    })
  },

  // Room Queue (R07a). Bearer-token auth via api.request; no client-supplied
  // identity fields are sent. Body shapes mirror the Go handler request
  // structs in internal/delivery/http/room_queue_handlers.go.
  async getRoomQueue(slug) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/queue`, { method: 'GET' })
  },

  async addRoomSong(slug, url, metadata = null) {
    const body = { url }
    if (metadata) body.metadata = metadata
    return this.request(`/rooms/${encodeURIComponent(slug)}/queue/add`, {
      method: 'POST',
      body
    })
  },

  async removeRoomSong(slug, index) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/queue/remove`, {
      method: 'POST',
      body: { index }
    })
  },

  async clearRoomQueue(slug) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/queue/clear`, {
      method: 'POST',
      body: {}
    })
  },

  // R07d: prioritize a non-current room song. Bearer-token auth via
  // api.request; the body MUST carry only song_index. Identity fields
  // (user_id, requested_by, etc.) are intentionally NOT honored when
  // present — backend ignores them. Server returns 204 No Content.
  async prioritizeRoomSong(slug, songIndex) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/queue/prioritize`, {
      method: 'POST',
      body: { song_index: songIndex }
    })
  },

  // --- R09a lease-aware room playback controls ---
  //
  // Each method targets a per-room endpoint; the backend enforces
  // active-room + active-membership + active-lease-holder. The
  // frontend does NOT pre-check the lease holder — the backend is
  // authoritative and surfaces 401/403/404/409/410 toasts the
  // RoomView can render verbatim. Body shapes mirror the R07d
  // pointer/zero-distinguishing convention: sync uses {elapsed}
  // (caller passes the integer; missing is rejected by the server
  // with 400) and status uses {status} ("playing" or "paused";
  // "idle" is rejected with 400).
  async setRoomPlaybackStatus(slug, status) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/playback/status`, {
      method: 'POST',
      body: { status }
    })
  },

  async syncRoomPlayback(slug, elapsed) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/playback/sync`, {
      method: 'POST',
      body: { elapsed }
    })
  },

  async skipRoomPlayback(slug) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/playback/skip`, {
      method: 'POST'
    })
  },

  async roomSongEnded(slug) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/playback/ended`, {
      method: 'POST'
    })
  },

  // R09c: room-scoped volume command. Lease-holder only (the backend is
  // authoritative and surfaces 401/403/404/409 toasts that mapPlaybackToast
  // already handles). Body: {direction: "up"|"down"}.
  async changeRoomPlaybackVolume(slug, direction) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/playback/volume`, {
      method: 'POST',
      body: { direction }
    })
  },

  // R09d: room-scoped previous playback command. Lease-holder only
  // (the backend is authoritative and surfaces 400/401/403/404/409/410
  // toasts that mapPlaybackToast already handles). Empty body. Returns
  // 400 on no-current-song or already-on-first; the backend does NOT
  // mutate, save, or broadcast on those error paths.
  async prevRoomPlayback(slug) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/playback/prev`, {
      method: 'POST'
    })
  },

  // --- R10c: room deletion and membership removal (frontend) ---
  //
  // Both methods target the R10b HTTP routes behind roomAuth.
  // Identity fields (user_id / requested_by / etc.) are intentionally
  // NOT honored on the wire — the backend ignores them and uses the
  // bearer-token-resolved user as the source of truth. No request body.
  //
  //   - deleteRoom: host-only soft archive. Idempotent 204 on
  //     already-archived rooms (no mutation, no broadcast).
  //   - removeRoomMember: host-only member removal. 400 for
  //     ErrHostCannotRemoveSelf + ErrCannotRemoveHost; 404 when
  //     target is not a member; 409 on archived room.
  async deleteRoom(slug) {
    return this.request(`/rooms/${encodeURIComponent(slug)}`, {
      method: 'DELETE'
    })
  },

  async removeRoomMember(slug, userId) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/members/${encodeURIComponent(String(userId))}`, {
      method: 'DELETE'
    })
  },

  // R10c member-list read surface. GET /api/rooms/{slug}/members behind
  // roomAuth. Returns { members: [{ user_id, role }, ...] } — the same
  // shape as the room_members_changed WebSocket envelope, minus
  // joined_at (intentionally omitted per R10a Decision 9). Any active
  // member may read; the backend enforces active-room + active-membership
  // (archived maps to 409, non-member maps to 403). No mutation, no
  // broadcast, no client-supplied identity fields.
  async getRoomMembers(slug) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/members`, {
      method: 'GET'
    })
  },

  // --- R11a: room chat (frontend) ---
  //
  // Both methods target the R11a HTTP routes behind roomAuth. The
  // sender identity is resolved server-side from the bearer token;
  // the request body NEVER carries a sender_id field. Active-room +
  // active-membership are enforced at the use-case layer (archived
  // maps to 409, non-member maps to 403).
  //
  //   - getRoomChatMessages: GET /api/rooms/{slug}/chat/messages?limit=50
  //     Returns { messages: [{ id, room_slug, sender: { user_id,
  //     display_name }, content, created_at }, ...] } ordered
  //     oldest → newest. The default limit is 50; the backend caps
  //     at 100 and rejects out-of-range limits with 400.
  //   - sendRoomChatMessage: POST /api/rooms/{slug}/chat/messages with
  //     body { content }. Returns 201 with the post-mutation envelope
  //     (same shape as a list entry). Sender email is NEVER returned.
  async getRoomChatMessages(slug, limit = 50) {
    const q = Number.isFinite(limit) && limit > 0 ? `?limit=${encodeURIComponent(String(limit))}` : ''
    return this.request(`/rooms/${encodeURIComponent(slug)}/chat/messages${q}`, {
      method: 'GET'
    })
  },

  async sendRoomChatMessage(slug, content) {
    return this.request(`/rooms/${encodeURIComponent(slug)}/chat/messages`, {
      method: 'POST',
      body: { content: String(content == null ? '' : content) }
    })
  },

  // --- R05b1: room entry, creation, and invite redemption UI ---
  //
  // Thin wrappers around the existing R04 / R10a backend routes. Bearer-token
  // auth via api.request; identity comes from the session, never the body.
  // Slugs and tokens are URL-encoded through encodeURIComponent so reserved
  // characters in a slug or token (slashes, spaces, +, =) survive the trip
  // without changing the path shape.
  //
  //   - createRoom: POST /rooms with body exactly { slug, name }. No
  //     user_id, no role, no display name on the wire. Returns the
  //     created entity.Room (id, slug, name, status, timestamps).
  //   - listRooms: GET /rooms?status=active. status defaults to "active"
  //     (the documented lifecycle filter). An intentionally empty status
  //     omits the query so callers can request all rooms; non-empty
  //     statuses are appended verbatim. Returns an array of entity.Room.
  //   - getRoom: GET /rooms/{encodedSlug}. No body. Used by the manual
  //     "open by slug" affordance to validate a slug before navigating.
  //     Returns the matching entity.Room.
  //   - redeemInvite: POST /invites/{encodedToken}/redeem with NO body.
  //     Used by the invite-redeem form. Returns the RoomMember (room_id,
  //     user_id, role, joined_at). The frontend uses membership.room_id
  //     + a refreshed active-room list to resolve the room slug for
  //     navigation (the wire does not carry the slug).
  //
  // Joining a room means invite redemption — there is no arbitrary
  // joinRoom endpoint, and api.joinRoom is intentionally NOT added.
  async createRoom(slug, name) {
    return this.request('/rooms', {
      method: 'POST',
      body: { slug: String(slug == null ? '' : slug), name: String(name == null ? '' : name) }
    })
  },

  async listRooms(status = 'active') {
    const q = status ? `?status=${encodeURIComponent(String(status))}` : ''
    return this.request(`/rooms${q}`, { method: 'GET' })
  },

  async getRoom(slug) {
    return this.request(`/rooms/${encodeURIComponent(String(slug == null ? '' : slug))}`, {
      method: 'GET'
    })
  },

  async redeemInvite(token) {
    return this.request(`/invites/${encodeURIComponent(String(token == null ? '' : token))}/redeem`, {
      method: 'POST'
    })
  },

  // --- R05b2: player lease (frontend) ---
  //
  // Thin wrappers around the existing player-lease routes registered
  // in cmd/server/main.go (lines 477–488). Bearer-token auth via
  // api.request; identity comes from the session, never the body. Slugs
  // are URL-encoded through encodeURIComponent so reserved characters
  // survive the trip without changing the path shape. None of the four
  // methods carries any client-supplied identity field — the backend
  // ignores them when present and uses the bearer-token-resolved user
  // as the source of truth.
  //
  //   - claimRoomPlayerLease: POST /api/rooms/{slug}/player/claim.
  //     Host-only. Returns the post-mutation entity.PlayerLease
  //     (id, room_id, claimed_by_user_id, claimed_at,
  //     last_heartbeat_at, expires_at, ended_at?). 409 if a valid
  //     lease already exists.
  //   - heartbeatRoomPlayerLease: POST
  //     /api/rooms/{slug}/player/heartbeat. Current-holder-only.
  //     Renews expires_at and returns the post-mutation lease. 403
  //     for non-holder, 410 past grace.
  //   - releaseRoomPlayerLease: POST
  //     /api/rooms/{slug}/player/release. Host-only. Ends the lease
  //     and archives the room. Returns 204 → null. 404 if no active
  //     lease exists (no mutation, no broadcast).
  //   - getRoomPlayerLease: GET /api/rooms/{slug}/player/lease. Any
  //     active member may read. Returns the current lease or 404
  //     when none exists.
  async claimRoomPlayerLease(slug) {
    return this.request(`/rooms/${encodeURIComponent(String(slug == null ? '' : slug))}/player/claim`, {
      method: 'POST'
    })
  },

  async heartbeatRoomPlayerLease(slug) {
    return this.request(`/rooms/${encodeURIComponent(String(slug == null ? '' : slug))}/player/heartbeat`, {
      method: 'POST'
    })
  },

  async releaseRoomPlayerLease(slug) {
    return this.request(`/rooms/${encodeURIComponent(String(slug == null ? '' : slug))}/player/release`, {
      method: 'POST'
    })
  },

  async getRoomPlayerLease(slug) {
    return this.request(`/rooms/${encodeURIComponent(String(slug == null ? '' : slug))}/player/lease`, {
      method: 'GET'
    })
  }
}
