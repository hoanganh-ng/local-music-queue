const API_BASE = window.location.port === '5173' ? 'http://localhost:1111/api' : '/api'

export const api = {
  async request(endpoint, options = {}) {
    const url = `${API_BASE}${endpoint}`

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

  // Queue Operations
  async getQueue() {
    return this.request('/queue', {
      method: 'GET'
    })
  },

  async addSong(url, addedBy) {
    return this.request('/queue/add', {
      method: 'POST',
      body: { url, added_by: addedBy }
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
  }
}
