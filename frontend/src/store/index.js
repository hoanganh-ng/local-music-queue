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

  // Add song to queue. Sprint 004: this is a pure splice + queue recalc.
  // The previous "if queue was empty, set as current" auto-promotion was
  // removed because backend now ships the authoritative current_index /
  // current_song / status on the same song_added event — letting the store
  // also infer them double-applied and could disagree with the backend.
  addSong(song, position) {
    // Insert song at specified position
    this.queueState.songs.splice(position, 0, song)
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
  },

  // --- R07c: per-room queue slice (isolated from global queue state) ---
  roomQueues: {},

  // Make sure a slug has an entry; returns the entry. R09g seeds the
  // per-room autoQueueConfig to the same default the R09f backend
  // uses (enabled=false, strategy='related') so reads before the
  // first status fetch still render a coherent "off" state.
  _ensureRoomEntry(slug) {
    if (!this.roomQueues[slug]) {
      this.roomQueues[slug] = {
        state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] },
        lastSeqNum: 0,
        recoveryInFlight: false,
        lastError: null,
        connected: false,
        autoQueueConfig: { enabled: false, strategy: 'related' },
      }
    }
    return this.roomQueues[slug]
  },

  setRoomQueueConnected(slug, connected) {
    this._ensureRoomEntry(slug).connected = !!connected
  },

  setRoomQueueError(slug, message) {
    if (!slug) return
    this._ensureRoomEntry(slug).lastError = message
  },

  setRoomQueueRecoveryInFlight(slug, inFlight) {
    this._ensureRoomEntry(slug).recoveryInFlight = !!inFlight
  },

  setRoomQueueSeq(slug, seq) {
    this._ensureRoomEntry(slug).lastSeqNum = seq
  },

  setRoomQueueState(slug, state) {
    const entry = this._ensureRoomEntry(slug)
    entry.state = state
    entry.lastError = null
  },

  applyRoomSongAdded(slug, song, position, fullState) {
    const entry = this._ensureRoomEntry(slug)
    if (fullState) {
      entry.state = fullState
      return
    }
    const s = entry.state
    s.songs = Array.isArray(s.songs) ? s.songs.slice() : []
    s.songs.splice(position, 0, song)
    if (typeof s.current_index !== 'number') s.current_index = -1
  },

  applyRoomSongRemoved(slug, removedIndex, fullState) {
    const entry = this._ensureRoomEntry(slug)
    if (fullState) {
      entry.state = fullState
      return
    }
    const s = entry.state
    if (removedIndex < 0 || removedIndex >= (s.songs || []).length) return
    s.songs = s.songs.slice()
    s.songs.splice(removedIndex, 1)
    if (typeof s.current_index !== 'number') return
    if (removedIndex < s.current_index) s.current_index--
    else if (removedIndex === s.current_index) {
      s.current_song = s.songs[s.current_index] || null
    }
  },

  applyRoomQueueCleared(slug, fullState) {
    const entry = this._ensureRoomEntry(slug)
    if (fullState) {
      entry.state = fullState
      return
    }
    const s = entry.state
    s.songs = []
    s.current_index = -1
    s.current_song = null
    s.status = 'stopped'
  },

  // R07d: applies a room_queue_song_prioritized event for slug.
  // Prefers the authoritative payload.state when present (same
  // contract as the other room mutators). The fallback path mirrors
  // entity.Queue.Prioritize: remove from fromIndex, insert at toIndex,
  // adjust current_index when the move crossed it.
  applyRoomSongPrioritized(slug, fromIndex, toIndex, song, fullState) {
    const entry = this._ensureRoomEntry(slug)
    if (fullState) {
      entry.state = fullState
      return
    }
    const s = entry.state
    const songs = (s.songs || []).slice()
    if (fromIndex < 0 || fromIndex >= songs.length) return
    if (toIndex < 0 || toIndex >= songs.length) return
    const [moved] = songs.splice(fromIndex, 1)
    songs.splice(toIndex, 0, { ...moved, ...song, is_prioritized: true })
    s.songs = songs
    if (typeof s.current_index === 'number') {
      if (fromIndex < s.current_index && toIndex >= s.current_index) {
        s.current_index--
      } else if (fromIndex > s.current_index && toIndex <= s.current_index) {
        s.current_index++
      }
    }
  },

  clearRoomQueueState(slug) {
    if (this.roomQueues[slug]) {
      delete this.roomQueues[slug]
    }
  },

  // --- R09a playback mutators ---
  //
  // Each mutator only touches globalStore.roomQueues[slug]. They MUST
  // NOT mutate globalStore.queueState, currentUser, voteSessions, or
  // autoQueueConfig. Every mutator prefers payload.state when present
  // (the broadcast's post-mutation snapshot is authoritative); the
  // fallback path mirrors entity.Queue semantics for the specific
  // mutation. Tests pin both the prefers-fullState and the fallback
  // branches, plus the isolation invariant.

  applyRoomPlaybackStatusChanged(slug, payload) {
    const entry = this._ensureRoomEntry(slug)
    if (payload && payload.state) {
      entry.state = payload.state
      return
    }
    const s = entry.state
    if (payload && typeof payload.status === 'string') {
      s.status = payload.status
    }
    if (payload && typeof payload.elapsed === 'number') {
      s.elapsed = payload.elapsed
    }
  },

  applyRoomPlaybackElapsedSync(slug, payload) {
    const entry = this._ensureRoomEntry(slug)
    if (payload && payload.state) {
      entry.state = payload.state
      return
    }
    const elapsed = typeof payload === 'number'
      ? payload
      : (payload && typeof payload.elapsed === 'number' ? payload.elapsed : null)
    if (elapsed !== null) {
      entry.state.elapsed = elapsed
    }
  },

  applyRoomPlaybackSongAdvanced(slug, payload) {
    const entry = this._ensureRoomEntry(slug)
    if (payload && payload.state) {
      entry.state = payload.state
      return
    }
    const s = entry.state
    if (!payload) return
    if (typeof payload.new_index === 'number') {
      s.current_index = payload.new_index
    }
    if (payload.current_song !== undefined) {
      s.current_song = payload.current_song
    }
    if (typeof payload.status === 'string') {
      s.status = payload.status
    }
    if (typeof payload.elapsed === 'number') {
      s.elapsed = payload.elapsed
    }
  },

  // R09d: applies a room_playback_song_previous event for slug.
  // Same contract as applyRoomPlaybackSongAdvanced: prefers
  // payload.state when present (authoritative post-mutation snapshot);
  // the fallback path mirrors entity.Queue.PrevToPrevious semantics
  // (decrement current_index, stamp the new current song, status,
  // elapsed) without touching the global queue state. The prev index
  // is intentionally NOT applied — only the new_index reflects the
  // current position after the move.
  applyRoomPlaybackSongPrevious(slug, payload) {
    const entry = this._ensureRoomEntry(slug)
    if (payload && payload.state) {
      entry.state = payload.state
      return
    }
    const s = entry.state
    if (!payload) return
    if (typeof payload.new_index === 'number') {
      s.current_index = payload.new_index
    }
    if (payload.current_song !== undefined) {
      s.current_song = payload.current_song
    }
    if (typeof payload.status === 'string') {
      s.status = payload.status
    }
    if (typeof payload.elapsed === 'number') {
      s.elapsed = payload.elapsed
    }
  },

  // --- R09g room auto-queue mutators ---
  //
  // All three mutators are isolated to globalStore.roomQueues[slug].
  // They MUST NOT touch globalStore.queueState, currentUser,
  // voteSessions, or autoQueueConfig. applyRoomAutoQueueAdded
  // mirrors the R09e payload contract: prefers payload.state when
  // present (the broadcast's post-mutation snapshot is authoritative);
  // the fallback path stamps current_index / current_song / status /
  // elapsed. applyRoomAutoQueueConfigChanged replaces the per-room
  // autoQueueConfig slice only.

  setRoomAutoQueueConfig(slug, enabled, strategy) {
    this._ensureRoomEntry(slug).autoQueueConfig = {
      enabled: !!enabled,
      strategy: strategy || 'related',
    }
  },

  applyRoomAutoQueueAdded(slug, payload) {
    const entry = this._ensureRoomEntry(slug)
    if (payload && payload.state) {
      entry.state = payload.state
      return
    }
    const s = entry.state
    if (!payload) return
    if (typeof payload.current_index === 'number') {
      s.current_index = payload.current_index
    }
    if (payload.current_song !== undefined) {
      s.current_song = payload.current_song
    }
    if (typeof payload.status === 'string') {
      s.status = payload.status
    }
    if (typeof payload.elapsed === 'number') {
      s.elapsed = payload.elapsed
    }
  },

  applyRoomAutoQueueConfigChanged(slug, payload) {
    if (!payload) return
    this._ensureRoomEntry(slug).autoQueueConfig = {
      enabled: !!payload.enabled,
      strategy: payload.strategy || 'related',
    }
  },
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
