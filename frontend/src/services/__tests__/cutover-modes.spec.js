import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// R14d cutover-modes: global WebSocket lifecycle in both baked modes.
//
// The cutover gate is a module-load-time constant, so each test resets
// the module registry, registers a doMock for the config module with
// the mode under test, and dynamically imports a fresh wsClient (and
// its fresh store). This keeps every case deterministic no matter
// which VITE_ROOM_CUTOVER_AUTHORITATIVE value the whole run is baked
// with, and one mode can never contaminate the other.
//
// The false-artifact message-handling behavior is pinned by
// websocket.spec.js; this file pins the R14d lifecycle contract:
//   - disconnect() marks the close intentional before closing;
//   - intentional close never schedules reconnection;
//   - existing reconnect timers are cancelled;
//   - status remains disconnected;
//   - false-mode connect() deliberately re-enables reconnection;
//   - true-mode connect() cannot open /ws;
//   - stale onclose callbacks cannot restart the global client.

const wsInstances = []
class FakeWS {
  constructor(url) {
    this.url = url
    this.readyState = 1 // OPEN
    this.close = vi.fn()
    this.send = vi.fn()
    wsInstances.push(this)
  }
}

function mockCutover(mode) {
  vi.doMock('../../config/cutover', () => ({
    parseRoomCutoverAuthoritative: (raw) => raw === 'true',
    roomCutoverAuthoritative: mode,
    authenticatedLandingRouteName: () => (mode ? 'RoomEntry' : 'Dashboard'),
  }))
}

async function loadClient(mode) {
  mockCutover(mode)
  const { wsClient } = await import('../websocket')
  const { globalStore } = await import('../../store')
  return { wsClient, globalStore }
}

describe('Global WebSocket cutover lifecycle (R14d)', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
    sessionStorage.clear()
    wsInstances.length = 0
    vi.stubGlobal('WebSocket', FakeWS)
    vi.useFakeTimers()
    vi.spyOn(console, 'log').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    vi.doUnmock('../../config/cutover')
  })

  // --- true (post-cutover) artifact ---

  it('true mode: accidental connect() does not construct a global WebSocket', async () => {
    const { wsClient, globalStore } = await loadClient(true)
    wsClient.connect()
    expect(wsInstances).toHaveLength(0)
    expect(wsClient.ws).toBeNull()
    expect(wsClient.isConnecting).toBe(false)
    expect(globalStore.connectionStatus).toBe('disconnected')
  })

  it('true mode: scheduleReconnect() never arms a timer', async () => {
    const { wsClient } = await loadClient(true)
    wsClient.scheduleReconnect()
    expect(wsClient.reconnectTimer).toBeNull()
    vi.advanceTimersByTime(60_000)
    expect(wsInstances).toHaveLength(0)
  })

  it('true mode: bootstrap-style disconnect() keeps status disconnected and stays disabled', async () => {
    const { wsClient, globalStore } = await loadClient(true)
    wsClient.disconnect()
    expect(wsClient.intentionalClose).toBe(true)
    expect(globalStore.connectionStatus).toBe('disconnected')
    wsClient.connect() // still cannot open /ws afterwards
    vi.advanceTimersByTime(60_000)
    expect(wsInstances).toHaveLength(0)
    expect(globalStore.connectionStatus).toBe('disconnected')
  })

  // --- false (pre-cutover / rollback) artifact ---

  it('false mode: connect() opens the global /ws normally', async () => {
    const { wsClient, globalStore } = await loadClient(false)
    wsClient.connect()
    expect(wsInstances).toHaveLength(1)
    expect(wsInstances[0].url).toContain('/ws')
    wsInstances[0].onopen()
    expect(globalStore.connectionStatus).toBe('connected')
  })

  it('false mode: disconnect() marks the close intentional BEFORE closing the socket', async () => {
    const { wsClient } = await loadClient(false)
    wsClient.connect()
    const socket = wsInstances[0]
    let intentionalAtClose = null
    socket.close.mockImplementation(() => {
      intentionalAtClose = wsClient.intentionalClose
    })
    wsClient.disconnect()
    expect(socket.close).toHaveBeenCalledTimes(1)
    expect(intentionalAtClose).toBe(true)
  })

  it('false mode: intentional close never schedules reconnection and status stays disconnected', async () => {
    const { wsClient, globalStore } = await loadClient(false)
    wsClient.connect()
    const socket = wsInstances[0]
    wsClient.disconnect()
    // The browser fires onclose asynchronously after close(); simulate it.
    socket.onclose()
    expect(wsClient.reconnectTimer).toBeNull()
    expect(globalStore.connectionStatus).toBe('disconnected')
    vi.advanceTimersByTime(60_000)
    expect(wsInstances).toHaveLength(1) // no reconnect attempt
  })

  it('false mode: disconnect() cancels an already-armed reconnect timer', async () => {
    const { wsClient, globalStore } = await loadClient(false)
    wsClient.connect()
    const socket = wsInstances[0]
    // Unintentional drop arms the 3s reconnect timer.
    socket.onclose()
    expect(globalStore.connectionStatus).toBe('reconnecting')
    expect(wsClient.reconnectTimer).not.toBeNull()
    wsClient.disconnect()
    expect(wsClient.reconnectTimer).toBeNull()
    expect(globalStore.connectionStatus).toBe('disconnected')
    vi.advanceTimersByTime(60_000)
    expect(wsInstances).toHaveLength(1) // stale timer never fired connect()
  })

  it('false mode: a stale onclose from a detached socket cannot restart the client', async () => {
    const { wsClient, globalStore } = await loadClient(false)
    wsClient.connect()
    const stale = wsInstances[0]
    wsClient.disconnect()
    // A fresh connect() deliberately re-enables normal reconnection...
    wsClient.connect()
    expect(wsInstances).toHaveLength(2)
    expect(wsClient.intentionalClose).toBe(false)
    wsInstances[1].onopen()
    // ...but the STALE socket's late onclose must not touch the new one.
    stale.onclose()
    expect(wsClient.ws).not.toBeNull()
    expect(wsClient.reconnectTimer).toBeNull()
    expect(globalStore.connectionStatus).toBe('connected')
  })

  it('false mode: unintentional drop still reconnects after 3 seconds (normal behavior preserved)', async () => {
    const { wsClient } = await loadClient(false)
    wsClient.connect()
    wsInstances[0].onopen()
    wsInstances[0].onclose()
    vi.advanceTimersByTime(3000)
    expect(wsInstances).toHaveLength(2)
  })
})
