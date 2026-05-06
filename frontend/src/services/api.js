const API_BASE = import.meta.env.VITE_API_BASE_URL || 'https://localhost:443'

export const api = {
  async request(endpoint, options = {}) {
    const url = `${API_BASE}/api${endpoint}`

    const defaultHeaders = {
      'Content-Type': 'application/json'
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
      throw new Error(errorText || `API Request failed with status ${response.status}`)
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
  }
}
