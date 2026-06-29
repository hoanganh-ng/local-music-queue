import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createRoomWsClient } from '../room-websocket'
import { sessionHelper } from '../session'

function makeFakeWS() {
  const inst = {
    readyState: 0,
    onopen: null,
    onmessage: null,
    onclose: null,
    onerror: null,
    sent: [],
    send: vi.fn(function(msg) { this.sent.push(msg) }),
    close: vi.fn(function() { this.readyState = 3; if (this.onclose) this.onclose() }),
  }
  return inst
}

describe('room-websocket', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    vi.restoreAllMocks()
  })

  it('connect URL uses /ws/rooms/{slug} and includes session_token when valid', () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession('tok-r07c', future)
    const wsInstances = []
    class FakeWS {
      constructor(url) { this.url = url; this.readyState = 0; this.close = vi.fn(); this.send = vi.fn(); wsInstances.push(this) }
    }
    vi.stubGlobal('WebSocket', FakeWS)
    const client = createRoomWsClient('lobby', {})
    client.connect()
    expect(wsInstances).toHaveLength(1)
    const url = wsInstances[0].url
    expect(url).toMatch(/^wss?:\/\/[^/]+\/ws\/rooms\/lobby\?session_token=tok-r07c$/)
    client.disconnect()
    vi.unstubAllGlobals()
  })

  it('connect URL works without a session token (read-only spectator)', () => {
    const wsInstances = []
    class FakeWS {
      constructor(url) { this.url = url; this.readyState = 0; this.close = vi.fn(); wsInstances.push(this) }
    }
    vi.stubGlobal('WebSocket', FakeWS)
    const client = createRoomWsClient('lobby', {})
    client.connect()
    expect(wsInstances).toHaveLength(1)
    expect(wsInstances[0].url).toMatch(/^wss?:\/\/[^/]+\/ws\/rooms\/lobby$/)
    client.disconnect()
    vi.unstubAllGlobals()
  })

  it('connect URL percent-encodes URL-sensitive characters in token', () => {
    const tricky = 'tok+with/sensitive=chars?&space %and#hash'
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession(tricky, future)
    const wsInstances = []
    class FakeWS {
      constructor(url) { this.url = url; this.readyState = 0; this.close = vi.fn(); wsInstances.push(this) }
    }
    vi.stubGlobal('WebSocket', FakeWS)
    const client = createRoomWsClient('lobby', {})
    client.connect()
    const url = wsInstances[0].url
    expect(url).not.toContain(tricky)
    expect(url).toContain('session_token=tok%2Bwith%2Fsensitive%3Dchars%3F%26space+%25and%23hash')
    client.disconnect()
    vi.unstubAllGlobals()
  })

  it('does NOT log raw URL or raw token (sanitized log only)', () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession('secret-tok-xyz', future)
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {})
    const wsInstances = []
    class FakeWS { constructor(url) { this.url = url; this.readyState = 0; this.close = vi.fn(); wsInstances.push(this) } }
    vi.stubGlobal('WebSocket', FakeWS)
    const client = createRoomWsClient('lobby', {})
    client.connect()
    for (const call of logSpy.mock.calls) {
      const flat = call.map(a => typeof a === 'string' ? a : JSON.stringify(a)).join(' ')
      expect(flat).not.toContain('secret-tok-xyz')
      expect(flat).not.toContain(wsInstances[0].url)
    }
    client.disconnect()
    logSpy.mockRestore()
    vi.unstubAllGlobals()
  })

  it('emits onMessage for parsed messages and tracks seq', () => {
    const ws = makeFakeWS()
    vi.stubGlobal('WebSocket', function() { return ws })
    const onMessage = vi.fn()
    const client = createRoomWsClient('lobby', { onMessage })
    client.connect()
    ws.onmessage({ data: JSON.stringify({ type: 'room_queue_sync', data: { state: { songs: [], current_index: -1 } }, seq_num: 5 }) })
    expect(onMessage).toHaveBeenCalledTimes(1)
    expect(client.lastSeqNum).toBe(5)
    client.disconnect()
    vi.unstubAllGlobals()
  })

  it('emits onGap when seq_num jumps and provides recoverable context', () => {
    const ws = makeFakeWS()
    vi.stubGlobal('WebSocket', function() { return ws })
    const onGap = vi.fn()
    const client = createRoomWsClient('lobby', { onGap })
    client.connect()
    ws.onmessage({ data: JSON.stringify({ type: 'room_queue_song_added', seq_num: 5 }) })
    ws.onmessage({ data: JSON.stringify({ type: 'room_queue_song_added', seq_num: 7 }) })
    expect(onGap).toHaveBeenCalledTimes(1)
    expect(onGap.mock.calls[0][0]).toMatchObject({ slug: 'lobby', lastSeqNum: 5, seqNum: 7 })
    expect(client.lastSeqNum).toBe(7)
    client.disconnect()
    vi.unstubAllGlobals()
  })

  it('first message (no lastSeqNum yet) does NOT trigger gap', () => {
    const ws = makeFakeWS()
    vi.stubGlobal('WebSocket', function() { return ws })
    const onGap = vi.fn()
    const client = createRoomWsClient('lobby', { onGap })
    client.connect()
    ws.onmessage({ data: JSON.stringify({ type: 'room_queue_sync', seq_num: 100 }) })
    expect(onGap).not.toHaveBeenCalled()
    expect(client.lastSeqNum).toBe(100)
    client.disconnect()
    vi.unstubAllGlobals()
  })

  it('never sends request_full_sync or any frame on seq gap', () => {
    const ws = makeFakeWS()
    vi.stubGlobal('WebSocket', function() { return ws })
    const onGap = vi.fn()
    const client = createRoomWsClient('lobby', { onGap })
    client.connect()
    ws.onmessage({ data: JSON.stringify({ type: 'room_queue_song_added', seq_num: 1 }) })
    ws.onmessage({ data: JSON.stringify({ type: 'room_queue_song_added', seq_num: 3 }) })
    expect(ws.sent).toEqual([])
    client.disconnect()
    vi.unstubAllGlobals()
  })

  it('disconnect closes socket and prevents later message delivery', () => {
    const ws = makeFakeWS()
    vi.stubGlobal('WebSocket', function() { return ws })
    const onMessage = vi.fn()
    const client = createRoomWsClient('lobby', { onMessage })
    client.connect()
    client.disconnect()
    expect(ws.close).toHaveBeenCalled()
    ws.onmessage({ data: JSON.stringify({ type: 'room_queue_sync', seq_num: 1 }) })
    expect(onMessage).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('emits onClose without throwing when socket errors', () => {
    let ctorWS = null
    vi.stubGlobal('WebSocket', function() {
      ctorWS = makeFakeWS()
      // Pre-close to trigger immediate close event.
      setTimeout(() => ctorWS.close(), 0)
      return ctorWS
    })
    const onClose = vi.fn()
    const onError = vi.fn()
    const client = createRoomWsClient('lobby', { onClose, onError })
    client.connect()
    return new Promise(resolve => {
      setTimeout(() => {
        expect(onClose).toHaveBeenCalled()
        client.disconnect()
        vi.unstubAllGlobals()
        resolve()
      }, 20)
    })
  })
})
