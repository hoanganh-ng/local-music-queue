import { globalStore } from '../store'
import { sessionHelper } from './session'

// hasAuthoritativeFields returns true when the backend payload carries the
// Sprint 004 authoritative post-mutation fields. Legacy backends omit them.
function hasAuthoritativeFields(data) {
  if (!data) return false
  return (
    typeof data.current_index === 'number' ||
    data.status !== undefined ||
    typeof data.elapsed === 'number'
  )
}

// applyAuthoritativeSnapshot applies the Sprint 004 post-mutation fields
// (current_index, current_song, status, elapsed) that song_added and
// auto_queue_added now carry. Each field is applied only when present, so
// older deployments missing the fields still work.
function applyAuthoritativeSnapshot(data) {
  if (!data) return
  if (typeof data.current_index === 'number') {
    globalStore.updateCurrentIndex(data.current_index, data.current_song || null)
  }
  if (data.status) {
    globalStore.updatePlaybackStatus(data.status)
  }
  if (typeof data.elapsed === 'number') {
    globalStore.updateElapsed(data.elapsed)
  }
}

// applyLegacyFirstSongFallback keeps the single behavior the legacy backend
// already implied by the absence of authoritative fields: when the queue had
// no current song and the just-inserted song is the first song in the queue,
// promote it to current, mark it as playing, and reset elapsed. This is the
// ONLY inference the frontend is permitted to make for a legacy payload.
// Exhausted-queue advancement and auto-queue "promote on paused" are NOT
// performed — those would require backend-authoritative state we don't have.
function applyLegacyFirstSongFallback(song) {
  const hadCurrent = !!globalStore.queueState.current_song
  const hadNoCurrentIndex = globalStore.queueState.current_index === -1 ||
    globalStore.queueState.current_index === undefined
  const isFirstSong = globalStore.queueState.songs.length === 1 &&
    globalStore.queueState.songs[0] && globalStore.queueState.songs[0].id === song.id
  if (!hadCurrent && hadNoCurrentIndex && isFirstSong) {
    globalStore.updateCurrentIndex(0, song)
    globalStore.updatePlaybackStatus('playing')
    globalStore.updateElapsed(0)
  }
}

function isUsableObject(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

class WebSocketClient {
  constructor() {
    this.ws = null
    this.reconnectTimer = null
    this.isConnecting = false
    this.lastSeqNum = 0
    this.pendingFullSync = false
    this.callbacks = {
      songAdded: [],
      voteEvent: []
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

  onVoteEvent(callback) {
    this.callbacks.voteEvent.push(callback)
    return () => {
      const index = this.callbacks.voteEvent.indexOf(callback)
      if (index > -1) {
        this.callbacks.voteEvent.splice(index, 1)
      }
    }
  }

  notifyVoteEvent(type, data) {
    this.callbacks.voteEvent.forEach(cb => {
      try {
        cb({ type, data })
      } catch (e) {
        console.error('Error in voteEvent callback:', e)
      }
    })
  }

  connect() {
    if (this.ws || this.isConnecting) return

    this.isConnecting = true
    globalStore.setConnectionStatus('connecting')

    // Get backend URL from environment and convert to WebSocket URL.
    const API_BASE = import.meta.env.VITE_API_BASE_URL || 'https://localhost:443'
    const wsBase = API_BASE.replace('https://', 'wss://').replace('http://', 'ws://') + '/ws'

    // A01: the backend no longer accepts ?user_id= for identity or
    // daily-priority attribution. Only the session_token is sent; clients
    // that omit it remain read-only spectators per R05.
    const params = new URLSearchParams()
    if (sessionHelper.isValid()) {
      params.set('session_token', sessionHelper.getToken())
    }
    const wsUrl = params.toString() ? `${wsBase}?${params.toString()}` : wsBase

    // Do not log the raw URL — it may contain a session token.
    console.log('Connecting to WebSocket')
    this.ws = new WebSocket(wsUrl)

    this.ws.onopen = () => {
      console.log('WebSocket connected')
      this.isConnecting = false
      this.pendingFullSync = false
      this.lastSeqNum = 0
      globalStore.setConnectionStatus('connected')
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
      this.pendingFullSync = false
      globalStore.setConnectionStatus('reconnecting')
      this.scheduleReconnect()
    }

    this.ws.onerror = (error) => {
      console.error('WebSocket error:', error)
      globalStore.setConnectionStatus('disconnected')
      this.ws.close()
    }
  }

  handleMessage(message) {
    // Track sequence number
    if (message.seq_num !== undefined) {
      // Detect gap (missed messages)
      if (this.lastSeqNum > 0 && message.seq_num > this.lastSeqNum + 1) {
        console.warn(`Sequence gap detected: ${this.lastSeqNum} -> ${message.seq_num}`)
        this.requestFullSync()
      }
      this.lastSeqNum = message.seq_num
    }

    switch (message.type) {
      case 'full_sync':
        this.pendingFullSync = false
        // Keep lastSeqNum at full_sync.seq_num (set by the top-level tracker
        // above) so the first delta (seq_num = full_sync.seq_num + 1) passes
        // the consecutive check, while a real gap (e.g. seq_num + 2) is still
        // detected. Resetting to 0 would blind the client to that gap.
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
        // Sprint 004: prefer backend-authoritative snapshot. If absent
        // (legacy backend), apply only the single approved fallback:
        // promote the just-inserted song to current if the queue was empty.
        if (hasAuthoritativeFields(message.data)) {
          applyAuthoritativeSnapshot(message.data)
        } else {
          applyLegacyFirstSongFallback(message.data.song)
        }
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
        if (isUsableObject(message.data.activity)) {
          globalStore.addActivity(message.data.activity)
        }
        this.notifyVoteEvent(message.type, message.data)
        break
      case 'vote_resolved':
        console.log(`Vote resolved (${message.data.outcome}):`, message.data.session_id)
        globalStore.removeVoteSession(message.data.session_id)
        if (isUsableObject(message.data.activity)) {
          globalStore.addActivity(message.data.activity)
        }
        this.notifyVoteEvent(message.type, message.data)
        break
      case 'auto_queue_added':
        if (message.data.song && message.data.song.id) {
          const position = globalStore.queueState.songs ? globalStore.queueState.songs.length : 0
          globalStore.addSong(message.data.song, position)
          globalStore.addActivity(message.data.activity)
          // Sprint 004: backend is the sole owner of current_index/status.
          // The frontend no longer promotes the newest auto-queued song to
          // current when the local store happens to be paused — that heuristic
          // raced the authoritative state. Apply only what the backend sent,
          // or the single approved first-song fallback when the legacy
          // payload lacks authoritative fields.
          if (hasAuthoritativeFields(message.data)) {
            applyAuthoritativeSnapshot(message.data)
          } else {
            applyLegacyFirstSongFallback(message.data.song)
          }
        }
        break
      case 'error':
        // R05 additive envelope: backend rejected a client-originated request
        // (e.g. request_full_sync without a valid session_token). Log a safe
        // warning and clear the pendingFullSync guard so the client does not
        // get stuck waiting for a full_sync that will never arrive. Never log
        // raw tokens or the raw message payload — only the backend code.
        console.warn('WebSocket server error:', message.data?.code || 'unknown')
        this.pendingFullSync = false
        break
      case 'auto_queue_config_changed':
        // Update auto-queue config state for all connected clients
        if (message.data.enabled !== undefined) {
          globalStore.updateAutoQueueConfig(message.data.enabled, message.data.strategy)
        }
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

  // requestFullSync sends a request_full_sync message to the backend to
  // recover from a detected sequence gap. A pending guard prevents repeated
  // gaps from spamming requests until the next full_sync arrives.
  requestFullSync() {
    if (this.pendingFullSync) return
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    this.pendingFullSync = true
    this.ws.send(JSON.stringify({ type: 'request_full_sync' }))
  }

  disconnect() {
    if (this.ws) {
      this.ws.close()
    }
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.ws = null
    this.isConnecting = false
    globalStore.setConnectionStatus('disconnected')
  }
}

export const wsClient = new WebSocketClient()
