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
  }
}
