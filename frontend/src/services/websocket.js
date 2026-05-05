import { globalStore } from '../store'

class WebSocketClient {
  constructor() {
    this.ws = null
    this.reconnectTimer = null
    this.isConnecting = false
    this.lastSeqNum = 0
    this.callbacks = {
      songAdded: []
    }
  }

  onSongAdded(callback) {
    this.callbacks.songAdded.push(callback)
    return () => {
      const index = this.callbacks.songAdded.indexOf(callback)
      if (index > -1) {
        this.callbacks.songAdded.splice(index, 1)
      }
    }
  }

  connect() {
    if (this.ws || this.isConnecting) return

    this.isConnecting = true

    // Get backend URL from environment and convert to WebSocket URL
    const API_BASE = import.meta.env.VITE_API_BASE_URL || 'https://localhost:443'
    let wsUrl = API_BASE.replace('https://', 'wss://').replace('http://', 'ws://') + '/ws'

    // Add user_id parameter if user is logged in
    const currentUser = globalStore.currentUser
    if (currentUser?.id) {
      wsUrl += `?user_id=${currentUser.id}`
    }

    console.log(`Connecting to WebSocket at ${wsUrl}`)
    this.ws = new WebSocket(wsUrl)

    this.ws.onopen = () => {
      console.log('WebSocket connected')
      this.isConnecting = false
      if (this.reconnectTimer) {
        clearTimeout(this.reconnectTimer)
        this.reconnectTimer = null
      }
    }

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        this.handleMessage(data)
      } catch (e) {
        console.error('Failed to parse WS message:', e)
      }
    }

    this.ws.onclose = () => {
      console.log('WebSocket disconnected. Attempting to reconnect in 3 seconds...')
      this.ws = null
      this.isConnecting = false
      this.scheduleReconnect()
    }

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error)
      this.ws.close()
    }
  }

  handleMessage(message) {
    // Track sequence number
    if (message.seq_num !== undefined) {
      // Detect gap (missed messages)
      if (this.lastSeqNum > 0 && message.seq_num > this.lastSeqNum + 1) {
        console.warn(`Sequence gap detected: ${this.lastSeqNum} -> ${message.seq_num}`)
        // For now, just log. Could request full sync here.
      }
      this.lastSeqNum = message.seq_num
    }

    switch (message.type) {
      case 'full_sync':
        globalStore.updateQueueState(message.data.state)
        break
      case 'user_joined':
        globalStore.addActivity(message.data.activity)
        break
      case 'song_added':
        // console.log('WebSocket song_added event:', message.data)
        if (!message.data.song || !message.data.song.id) {
          console.error('Received invalid song object:', message.data.song)
          break
        }
        globalStore.addSong(message.data.song, message.data.position)
        globalStore.addActivity(message.data.activity)
        this.callbacks.songAdded.forEach(cb => {
          try {
            cb(message.data.song)
          } catch (e) {
            console.error('Error in songAdded callback:', e)
          }
        })
        break
      case 'song_skipped':
        globalStore.updateCurrentIndex(message.data.new_index, message.data.current_song)
        globalStore.updatePlaybackStatus(message.data.status)
        globalStore.updateElapsed(message.data.elapsed)
        globalStore.addActivity(message.data.activity)
        break
      case 'status_changed':
        globalStore.updatePlaybackStatus(message.data.status)
        globalStore.updateElapsed(message.data.elapsed)
        globalStore.addActivity(message.data.activity)
        break
      case 'elapsed_sync':
        globalStore.updateElapsed(message.data.elapsed)
        break
      case 'song_previous':
        globalStore.updateCurrentIndex(message.data.new_index, message.data.current_song)
        globalStore.updatePlaybackStatus(message.data.status)
        globalStore.updateElapsed(message.data.elapsed)
        globalStore.addActivity(message.data.activity)
        break
      case 'song_removed':
        globalStore.removeSong(message.data.removed_index)
        globalStore.updatePlaybackStatus(message.data.status)
        globalStore.addActivity(message.data.activity)
        break
      case 'queue_cleared':
        globalStore.clearQueue()
        globalStore.updatePlaybackStatus(message.data.status)
        globalStore.addActivity(message.data.activity)
        break
      case 'volume_changed':
        globalStore.handleVolumeChange(message.data.direction)
        break
      case 'song_prioritized':
        globalStore.prioritizeSong(
          message.data.from_index,
          message.data.to_index,
          message.data.song
        )
        globalStore.addActivity(message.data.activity)

        if (globalStore.currentUser?.id === message.data.user_id) {
          globalStore.updatePriorityBalance(message.data.user_balance)
        }
        break
      case 'priority_balance_updated':
        if (globalStore.currentUser?.id === message.data.user_id) {
          globalStore.updatePriorityBalance(message.data.balance)
        }
        break
      case 'vote_updated':
        globalStore.upsertVoteSession(message.data.session)
        globalStore.addActivity(message.data.activity)
        break
      case 'vote_resolved':
        console.log(`Vote resolved (${message.data.outcome}):`, message.data.session_id)
        globalStore.removeVoteSession(message.data.session_id)
        globalStore.addActivity(message.data.activity)
        break
      // Keep backward compatibility
      case 'queue_updated':
      case 'status_updated':
        if (message.state) {
          globalStore.updateQueueState(message.state)
        }
        break
      default:
        console.warn('Unknown message type:', message.type)
    }
  }

  scheduleReconnect() {
    if (!this.reconnectTimer) {
      this.reconnectTimer = setTimeout(() => {
        this.reconnectTimer = null
        this.connect()
      }, 3000)
    }
  }

  disconnect() {
    if (this.ws) {
      this.ws.close()
    }
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
    }
  }
}

export const wsClient = new WebSocketClient()
