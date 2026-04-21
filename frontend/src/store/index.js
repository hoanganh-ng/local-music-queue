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

export const globalStore = reactive({
  currentUser: initialUser,
  queueState: {
    status: 'stopped', // playing, paused, stopped
    current_song: null,
    queue: [],
    history: []
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
    const songs = newState.songs || []
    const currentIndex = newState.current_index !== undefined ? newState.current_index : -1

    // Map backend 'idle' to 'stopped' for UI consistency
    this.queueState.status = newState.status === 'idle' ? 'stopped' : (newState.status || this.queueState.status)
    this.queueState.history = newState.history || this.queueState.history

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

  // NEW: Update elapsed time
  updateElapsed(elapsed) {
    this.queueState.elapsed = elapsed
  },

  // NEW: Add single activity to history
  addActivity(activity) {
    // Add to beginning (newest first)
    this.queueState.history.unshift(activity)

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
  
  updatePlaybackStatus(status) {
    this.queueState.status = status === 'idle' ? 'stopped' : status
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
