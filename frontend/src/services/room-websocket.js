import { sessionHelper } from './session'

const API_BASE = import.meta.env.VITE_API_BASE_URL || 'https://localhost:443'

function apiBaseToWsBase(base) {
  if (base.startsWith('https://')) return 'wss://' + base.slice('https://'.length)
  if (base.startsWith('http://')) return 'ws://' + base.slice('http://'.length)
  return base
}

export function createRoomWsClient(slug, { onMessage, onGap, onOpen, onClose, onError } = {}) {
  const wsBase = apiBaseToWsBase(API_BASE) + '/ws/rooms/' + encodeURIComponent(slug)
  const params = new URLSearchParams()
  if (sessionHelper.isValid()) {
    params.set('session_token', sessionHelper.getToken())
  }
  const qs = params.toString() ? `?${params.toString()}` : ''
  const url = `${wsBase}${qs}`

  let ws = null
  let disposed = false
  let lastSeqNum = 0
  let hasPriorSeq = false

  function logSafe(...args) {
    // Never include the raw URL or raw token in any log.
    console.log('room ws:', ...args)
  }

  function notifyGap(seqNum) {
    if (typeof onGap === 'function') {
      try { onGap({ slug, lastSeqNum: hasPriorSeq ? lastSeqNum : 0, seqNum }) } catch (e) { /* swallow in client */ }
    }
  }

  return {
    get lastSeqNum() { return lastSeqNum },
    get hasPriorSeq() { return hasPriorSeq },
    get ws() { return ws },
    send(_msg) { return false }, // Room hub ignores inbound frames; never send.

    isOpen() { return !!ws && ws.readyState === 1 /* OPEN */ },

    connect() {
      if (disposed) return
      if (ws) return
      // Sanitized log: include slug only, never the URL or token.
      logSafe('connecting', { slug })
      ws = new WebSocket(url)
      ws.onopen = () => {
        if (disposed) return
        lastSeqNum = 0
        hasPriorSeq = false
        if (typeof onOpen === 'function') {
          try { onOpen() } catch (e) { /* swallow */ }
        }
      }
      ws.onmessage = (event) => {
        if (disposed) return
        let msg
        try { msg = JSON.parse(event.data) } catch (e) {
          console.error('room ws: malformed message')
          return
        }
        const seq = msg.seq_num
        if (typeof seq === 'number') {
          if (hasPriorSeq && seq > lastSeqNum + 1) {
            notifyGap(seq)
          }
          lastSeqNum = seq
          hasPriorSeq = true
        }
        if (typeof onMessage === 'function') {
          try { onMessage(msg) } catch (e) { console.error('room ws: handler threw', e) }
        }
      }
      ws.onerror = (err) => {
        if (typeof onError === 'function') {
          try { onError(err) } catch (e) { /* swallow */ }
        }
      }
      ws.onclose = (event) => {
        ws = null
        if (disposed) return
        if (typeof onClose === 'function') {
          // Pass the raw CloseEvent so callers can inspect the close
          // code (e.g. 1008 = policy violation on host-driven removal).
          // The CloseEvent is intentionally narrow: it carries `code`,
          // `reason`, and `wasClean`. Callers that don't care about
          // the close code continue to work unchanged because the
          // previous contract accepted an argument-less onClose().
          try { onClose(event) } catch (e) { /* swallow */ }
        }
      }
    },

    disconnect() {
      disposed = true
      if (ws) {
        try { ws.close() } catch (e) { /* ignore */ }
        ws = null
      }
    },
  }
}
