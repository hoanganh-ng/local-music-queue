import { describe, it, expect, beforeEach } from 'vitest'
import { globalStore } from '../../store'
import { sessionHelper } from '../../services/session'

// R14d: retireLegacyGlobalRuntimeState() contract.
//
// The operation resets the four legacy global slices to their
// documented initial values via fresh-state factories and MUST
// preserve currentUser, the exact roomQueues object (same identity,
// every room entry intact), and session storage. The store module
// does not read the cutover gate, so this suite is deterministic
// under both baked env values.

function populateLegacySlices() {
  globalStore.queueState = {
    status: 'playing',
    current_song: { id: 'song-1', title: 'Now Playing' },
    queue: [{ id: 'song-2', title: 'Up Next' }],
    history: [{ id: 'song-0', title: 'Played' }],
  }
  globalStore.voteSessions = { 7: { id: 7, type: 'skip', votes: 2 } }
  globalStore.autoQueueConfig = { enabled: true, strategy: 'popular' }
  globalStore.connectionStatus = 'connected'
}

describe('globalStore.retireLegacyGlobalRuntimeState (R14d)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.clearUser()
    globalStore.retireLegacyGlobalRuntimeState()
    for (const slug of Object.keys(globalStore.roomQueues)) {
      delete globalStore.roomQueues[slug]
    }
  })

  it('resets the legacy global slices to their exact documented initial values', () => {
    populateLegacySlices()
    globalStore.retireLegacyGlobalRuntimeState()

    expect(globalStore.queueState).toEqual({
      status: 'stopped',
      current_song: null,
      queue: [],
      history: [],
    })
    expect(globalStore.voteSessions).toEqual({})
    expect(globalStore.autoQueueConfig).toEqual({ enabled: false, strategy: 'related' })
    expect(globalStore.connectionStatus).toBe('disconnected')
  })

  it('preserves currentUser and session storage', () => {
    const future = new Date(Date.now() + 60 * 60_000).toISOString()
    sessionHelper.saveSession('opaque-token', future)
    globalStore.setUser({ id: 5, display_name: 'Keep Me', role: 'host' })
    populateLegacySlices()

    globalStore.retireLegacyGlobalRuntimeState()

    expect(globalStore.currentUser).toEqual({ id: 5, display_name: 'Keep Me', role: 'host' })
    expect(sessionHelper.getToken()).toBe('opaque-token')
    expect(sessionHelper.isValid()).toBe(true)
  })

  it('preserves the exact roomQueues object identity and every room entry with its content', () => {
    const lobby = globalStore._ensureRoomEntry('lobby')
    lobby.state.songs.push({ id: 'r-song', title: 'Room Song' })
    lobby.lastSeqNum = 12
    const lounge = globalStore._ensureRoomEntry('lounge')
    const roomQueuesRef = globalStore.roomQueues
    const lobbySnapshot = JSON.parse(JSON.stringify(globalStore.roomQueues.lobby))
    populateLegacySlices()

    globalStore.retireLegacyGlobalRuntimeState()

    expect(globalStore.roomQueues).toBe(roomQueuesRef) // same object identity
    expect(globalStore.roomQueues.lobby).toBe(lobby) // entry identity intact
    expect(globalStore.roomQueues.lounge).toBe(lounge)
    expect(JSON.parse(JSON.stringify(globalStore.roomQueues.lobby))).toEqual(lobbySnapshot)
    expect(globalStore.roomQueues.lobby.lastSeqNum).toBe(12)
  })

  it('uses fresh state factories — repeated resets never share mutable objects', () => {
    globalStore.retireLegacyGlobalRuntimeState()
    const firstQueueState = globalStore.queueState
    const firstVoteSessions = globalStore.voteSessions
    const firstAutoQueue = globalStore.autoQueueConfig
    firstQueueState.queue.push({ id: 'mutated' })
    firstVoteSessions[1] = { id: 1 }
    firstAutoQueue.enabled = true

    globalStore.retireLegacyGlobalRuntimeState()

    expect(globalStore.queueState).not.toBe(firstQueueState)
    expect(globalStore.voteSessions).not.toBe(firstVoteSessions)
    expect(globalStore.autoQueueConfig).not.toBe(firstAutoQueue)
    expect(globalStore.queueState.queue).toEqual([])
    expect(globalStore.voteSessions).toEqual({})
    expect(globalStore.autoQueueConfig).toEqual({ enabled: false, strategy: 'related' })
  })
})
