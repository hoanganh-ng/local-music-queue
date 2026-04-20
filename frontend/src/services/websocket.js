import { globalStore } from '../store'

class WebSocketClient {
  constructor() {
    this.ws = null
    this.reconnectTimer = null
    this.isConnecting = false
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
    switch (message.type) {
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
