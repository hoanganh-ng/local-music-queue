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

    // Determine WS protocol based on current location protocol
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    // Get host, fallback to localhost:8080 if not running on the same port
    const host = window.location.port === '5173' ? 'localhost:1111' : window.location.host
    const wsUrl = `${protocol}//${host}/ws`

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
        console.log('WebSocket song_added event:', message.data)
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
