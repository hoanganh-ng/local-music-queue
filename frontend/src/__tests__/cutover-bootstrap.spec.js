import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'

// R14d cutover-modes: bootstrap behavior of main.js in both baked modes.
//
// main.js runs its cutover block at import time, so each test resets
// the module registry, doMocks the config module (and main.js's other
// collaborators), and dynamically imports a fresh main.js. This keeps
// both cases deterministic no matter which env value the run is baked
// with.
//
// Contract under test:
//   true  → retire legacy global runtime state, then intentionally
//           disconnect the global WebSocket, then mount;
//   false → neither happens: populated global state is NOT reset and
//           the global WebSocket is NOT force-disconnected.

const retireMock = vi.fn()
const disconnectMock = vi.fn()
const connectMock = vi.fn()
const routerInstallMock = vi.fn()

function mockBootstrapCollaborators(mode) {
  vi.doMock('../config/cutover', () => ({
    parseRoomCutoverAuthoritative: (raw) => raw === 'true',
    roomCutoverAuthoritative: mode,
    authenticatedLandingRouteName: () => (mode ? 'RoomEntry' : 'Dashboard'),
  }))
  vi.doMock('../store', () => ({
    globalStore: { retireLegacyGlobalRuntimeState: retireMock },
  }))
  vi.doMock('../services/websocket', () => ({
    wsClient: { connect: connectMock, disconnect: disconnectMock },
  }))
  vi.doMock('../router', () => ({
    default: { install: routerInstallMock },
  }))
  vi.doMock('../App.vue', () => ({
    default: { template: '<div data-testid="app-stub" />' },
  }))
}

describe('Bootstrap (main.js) cutover behavior (R14d)', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.clearAllMocks()
    document.body.innerHTML = '<div id="app"></div>'
  })

  afterEach(() => {
    vi.doUnmock('../config/cutover')
    vi.doUnmock('../store')
    vi.doUnmock('../services/websocket')
    vi.doUnmock('../router')
    vi.doUnmock('../App.vue')
    document.body.innerHTML = ''
  })

  it('true mode: retires legacy global runtime state, then intentionally disconnects the global WebSocket, then mounts', async () => {
    mockBootstrapCollaborators(true)
    await import('../main.js')

    expect(retireMock).toHaveBeenCalledTimes(1)
    expect(disconnectMock).toHaveBeenCalledTimes(1)
    expect(connectMock).not.toHaveBeenCalled()
    // Retirement happens BEFORE the WebSocket disconnect.
    expect(retireMock.mock.invocationCallOrder[0]).toBeLessThan(
      disconnectMock.mock.invocationCallOrder[0]
    )
    // The app is mounted with the cutover-aware router.
    expect(routerInstallMock).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[data-testid="app-stub"]')).not.toBeNull()
  })

  it('false mode: does not reset global state and does not force-disconnect the global WebSocket', async () => {
    mockBootstrapCollaborators(false)
    await import('../main.js')

    expect(retireMock).not.toHaveBeenCalled()
    expect(disconnectMock).not.toHaveBeenCalled()
    // Current application behavior is preserved: the app still mounts.
    expect(routerInstallMock).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[data-testid="app-stub"]')).not.toBeNull()
  })
})
