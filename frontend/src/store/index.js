import { reactive, watch } from 'vue'

const STORE_KEY = 'lmq_user_session'

// Load initial user from localStorage
let initialUser = null
try {
  const stored = localStorage.getItem(STORE_KEY)
  if (stored) {
    initialUser = JSON.parse(stored)
  }
} catch (e) {
  console.error('Failed to parse stored user session', e)
}

const md5 = (str) => {
  // Simple MD5 hash function for generating keys (not cryptographically secure)
  // Source: https://stackoverflow.com/a/16552171
  return str.split('').reduce((hash, char) => {
    hash = ((hash << 5) - hash) + char.charCodeAt(0)
    return hash & hash
  }, 0).toString()
}

export const globalStore = reactive({
  currentUser: initialUser,
  queueState: {
    status: 'stopped', // playing, paused, stopped
    current_song: null,
    queue: [],
    history: []
  },
  voteSessions: {}, // key: session.id → session object
  autoQueueConfig: { enabled: false, strategy: 'related' }, // auto-queue config state
  connectionStatus: 'disconnected',

  setConnectionStatus(status) {
    this.connectionStatus = status
  },

  setUser(user) {
    this.currentUser = user
  },

  clearUser() {
    this.currentUser = null
  },

  updateQueueState(newState) {
    if (!newState) return

    // Map backend 'songs' to our 'queue' (Up Next)
    // and derive 'current_song' from 'current_index'
    const songs = newState.songs || (newState.current_song ? [newState.current_song, ...(newState.queue || [])] : (newState.queue || []))
    const currentIndex = newState.current_index !== undefined ? newState.current_index : (newState.current_song ? 0 : -1)

    // Map backend 'idle' to 'stopped' for UI consistency
    this.queueState.status = newState.status === 'idle' ? 'stopped' : (newState.status || this.queueState.status)
    this.queueState.history = (newState.history || this.queueState.history || []).map((s) => ({ ...s, key: md5(s.timestamp + s.type) }))

    if (currentIndex >= 0 && currentIndex < songs.length) {
      this.queueState.current_song = songs[currentIndex]
      // 'queue' in our store represents "Up Next"
      this.queueState.queue = songs.slice(currentIndex + 1)
    } else {
      this.queueState.current_song = null
      // If nothing is playing, all songs are technically "Up Next"
      this.queueState.queue = songs
    }

    // Store full list and index for reference
    this.queueState.songs = songs
    this.queueState.current_index = currentIndex
  },

  // NEW: Add song to queue
  addSong(song, position) {
    // Insert song at specified position
    this.queueState.songs.splice(position, 0, song)

    // If queue was empty, set as current song
    if (this.queueState.current_index === -1 && this.queueState.songs.length === 1) {
      this.queueState.current_index = 0
      this.queueState.current_song = song
      this.queueState.status = 'playing'
    }

    // Recalculate "Up Next" queue
    this.recalculateQueue()
  },

  // NEW: Update current index (for skip)
  updateCurrentIndex(newIndex, currentSong) {
    this.queueState.current_index = newIndex
    this.queueState.current_song = currentSong
    this.recalculateQueue()
  },

  // NEW: Update playback status
  updatePlaybackStatus(status) {
    this.queueState.status = status === 'idle' ? 'stopped' : status
  },

  // NEW: Remove song by index
  removeSong(index) {
    if (index < 0 || index >= this.queueState.songs.length) return

    this.queueState.songs.splice(index, 1)

    // Adjust current index if necessary (mirroring backend logic)
    if (index < this.queueState.current_index) {
      this.queueState.current_index--
    } else if (index === this.queueState.current_index) {
      if (this.queueState.songs.length === 0) {
        this.queueState.current_index = -1
        this.queueState.current_song = null
        this.queueState.status = 'stopped'
        this.queueState.elapsed = 0
      } else if (this.queueState.current_index >= this.queueState.songs.length) {
        this.queueState.current_index = this.queueState.songs.length - 1
        this.queueState.current_song = this.queueState.songs[this.queueState.current_index]
        this.queueState.elapsed = 0
      } else {
        // A new song takes its place at same index
        this.queueState.current_song = this.queueState.songs[this.queueState.current_index]
        this.queueState.elapsed = 0
      }
    }

    this.recalculateQueue()
  },

  // NEW: Clear queue (keep only current)
  clearQueue() {
    if (this.queueState.current_index === -1 || this.queueState.songs.length === 0) {
      this.queueState.songs = []
      this.queueState.current_index = -1
      this.queueState.current_song = null
      this.queueState.status = 'stopped'
      this.queueState.elapsed = 0
    } else {
      const currentSong = this.queueState.songs[this.queueState.current_index]
      this.queueState.songs = [currentSong]
      this.queueState.current_index = 0
    }
    this.recalculateQueue()
  },

  // NEW: Update elapsed time
  updateElapsed(elapsed) {
    this.queueState.elapsed = elapsed
  },

  // NEW: Add single activity to history
  addActivity(activity) {
    // Add to beginning (newest first)
    this.queueState.history.unshift({ ...activity, key: md5(activity.timestamp + activity.type) })

    // Keep only last 50 activities
    if (this.queueState.history.length > 50) {
      this.queueState.history = this.queueState.history.slice(0, 50)
    }
  },

  // NEW: Helper to recalculate "Up Next" queue
  recalculateQueue() {
    const currentIndex = this.queueState.current_index
    const songs = this.queueState.songs

    if (currentIndex >= 0 && currentIndex < songs.length) {
      this.queueState.queue = songs.slice(currentIndex + 1)
    } else {
      this.queueState.queue = songs
    }
  },

  // NEW: Handle volume change event
  handleVolumeChange(direction) {
    this.queueState.volumeChangeDirection = direction
    this.queueState.volumeChangeTimestamp = Date.now()
  },

  // NEW: Prioritize song
  prioritizeSong(fromIndex, toIndex, song) {
    // Remove from old position
    this.queueState.songs.splice(fromIndex, 1)

    // Insert at new position
    this.queueState.songs.splice(toIndex, 0, song)

    // Adjust current_index if needed
    if (fromIndex < this.queueState.current_index && toIndex >= this.queueState.current_index) {
      this.queueState.current_index--
    } else if (fromIndex > this.queueState.current_index && toIndex <= this.queueState.current_index) {
      this.queueState.current_index++
    }

    this.recalculateQueue()
  },

  // NEW: Update priority balance
  updatePriorityBalance(balance) {
    if (this.currentUser) {
      this.currentUser.priority_balance = balance
    }
  },

  // Vote session management
  upsertVoteSession(session) {
    // Reassign the whole object to ensure reactivity triggers
    this.voteSessions = { ...this.voteSessions, [session.id]: session }
  },

  removeVoteSession(sessionID) {
    // Create a new object and reassign to ensure reactivity triggers
    const newSessions = { ...this.voteSessions }
    delete newSessions[sessionID]
    this.voteSessions = newSessions
  },

  // Auto-queue config management
  updateAutoQueueConfig(enabled, strategy) {
    // Store config for reactive updates across components
    this.autoQueueConfig = { enabled, strategy }
  },

  // Convenience: return skip session for a song ID, or null
  skipSessionFor(songID) {
    return this.voteSessions[`skip:${songID}`] || null
  },

  // Convenience: return priority session for a song ID, or null
  prioritySessionFor(songID) {
    return this.voteSessions[`prioritize:${songID}`] || null
  }
})

// Watch for changes to currentUser and persist to localStorage
watch(
  () => globalStore.currentUser,
  (newUser) => {
    if (newUser) {
      localStorage.setItem(STORE_KEY, JSON.stringify(newUser))
    } else {
      localStorage.removeItem(STORE_KEY)
    }
  },
  { deep: true }
)
