import { describe, it, expect, vi, beforeEach } from 'vitest'
import { wsClient } from '../websocket'
import { globalStore } from '../../store'
import { sessionHelper } from '../session'

// Mutable queueState so the legacy fallback can read it during tests.
const liveQueueState = { songs: [], status: 'paused', current_index: -1 }

// Mock the globalStore with the surface websocket.js touches.
vi.mock('../../store', () => ({
  globalStore: {
    get queueState() { return liveQueueState },
    set queueState(v) { Object.assign(liveQueueState, v) },
    updateQueueState: vi.fn(),
    addSong: vi.fn((song) => { liveQueueState.songs.push(song) }),
    addActivity: vi.fn(),
    upsertVoteSession: vi.fn(),
    removeVoteSession: vi.fn(),
    updateCurrentIndex: vi.fn((idx, song) => {
      liveQueueState.current_index = idx
      liveQueueState.current_song = song
    }),
    updatePlaybackStatus: vi.fn((status) => {
      liveQueueState.status = status
    }),
    updateElapsed: vi.fn((elapsed) => {
      liveQueueState.elapsed = elapsed
    }),
    setConnectionStatus: vi.fn(),
  }
}))

describe('WebSocketClient', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    liveQueueState.songs = []
    liveQueueState.status = 'paused'
    liveQueueState.current_index = -1
    liveQueueState.current_song = null
    liveQueueState.elapsed = 0
    wsClient.callbacks.voteEvent = []
    // Reset sequence-gap recovery state between tests.
    wsClient.lastSeqNum = 0
    wsClient.pendingFullSync = false
    wsClient.ws = null
  })

  it('should handle queue_updated message', () => {
    const mockState = { status: 'playing', queue: [] }
    const message = {
      type: 'queue_updated',
      state: mockState
    }

    wsClient.handleMessage(message)

    expect(globalStore.updateQueueState).toHaveBeenCalledWith(mockState)
  })

  it('should handle status_updated message', () => {
    const mockState = { status: 'paused' }
    const message = {
      type: 'status_updated',
      state: mockState
    }

    wsClient.handleMessage(message)

    expect(globalStore.updateQueueState).toHaveBeenCalledWith(mockState)
  })

  it('should ignore unknown message types', () => {
    const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const message = { type: 'unknown_type' }

    wsClient.handleMessage(message)

    expect(globalStore.updateQueueState).not.toHaveBeenCalled()
    expect(consoleSpy).toHaveBeenCalledWith('Unknown message type:', 'unknown_type')

    consoleSpy.mockRestore()
  })

  // --- Sprint 004 ---

  it('song_added applies authoritative current_index, current_song, status, elapsed', () => {
    const song = { id: 'newone', title: 'New' }
    const currentSong = { id: 'existing', title: 'Existing' }
    wsClient.handleMessage({
      type: 'song_added',
      data: {
        song,
        position: 3,
        activity: { type: 'song_added', user: 'Alice' },
        current_index: 0,
        current_song: currentSong,
        status: 'playing',
        elapsed: 12,
      },
    })

    expect(globalStore.addSong).toHaveBeenCalledWith(song, 3)
    expect(globalStore.updateCurrentIndex).toHaveBeenCalledWith(0, currentSong)
    expect(globalStore.updatePlaybackStatus).toHaveBeenCalledWith('playing')
    expect(globalStore.updateElapsed).toHaveBeenCalledWith(12)
  })

  it('auto_queue_added applies authoritative snapshot instead of promoting on paused', () => {
    // Local store reports paused — the old heuristic would have promoted the
    // new song to current_index. Sprint 004: backend sends authoritative
    // current_index unchanged, and the frontend respects it.
    globalStore.queueState = { songs: [{ id: 'src' }], status: 'paused', current_index: 0 }
    const song = { id: 'auto', title: 'Auto-Queued' }
    const currentSong = { id: 'src', title: 'Source' }
    wsClient.handleMessage({
      type: 'auto_queue_added',
      data: {
        song,
        source_song_title: 'Source',
        activity: { type: 'song_added', user: 'Auto-Queue' },
        current_index: 0,
        current_song: currentSong,
        status: 'paused',
        elapsed: 0,
      },
    })

    expect(globalStore.addSong).toHaveBeenCalledWith(song, 1)
    // Backend says current_index stays 0 — frontend must NOT shift to 1.
    expect(globalStore.updateCurrentIndex).toHaveBeenCalledWith(0, currentSong)
    expect(globalStore.updatePlaybackStatus).toHaveBeenCalledWith('paused')
    // updatePlaybackStatus is never called with 'playing' for this message.
    expect(
      globalStore.updatePlaybackStatus.mock.calls.find(c => c[0] === 'playing')
    ).toBeUndefined()
  })

  it('legacy song_added with first song promotes it to current and starts playback', () => {
    // Stateful assertion: empty queue, no current song. Legacy payload
    // (no authoritative fields). After handling, the inserted song is
    // promoted to current, status is playing, elapsed is 0.
    liveQueueState.songs = []
    liveQueueState.current_index = -1
    liveQueueState.current_song = null
    liveQueueState.status = 'stopped'

    const song = { id: 'first', title: 'First' }
    wsClient.handleMessage({
      type: 'song_added',
      data: {
        song,
        position: 0,
        activity: { type: 'song_added', user: 'Alice' },
      },
    })

    expect(globalStore.addSong).toHaveBeenCalledWith(song, 0)
    // The legacy fallback promotes the inserted song to current and starts
    // playback. These calls happen via the SAME authoritative setters the
    // Sprint 004 path uses — but the trigger is the frontend's first-song
    // inference, not the backend. Verify the exact arguments.
    expect(globalStore.updateCurrentIndex).toHaveBeenCalledTimes(1)
    expect(globalStore.updateCurrentIndex).toHaveBeenCalledWith(0, song)
    expect(globalStore.updatePlaybackStatus).toHaveBeenCalledTimes(1)
    expect(globalStore.updatePlaybackStatus).toHaveBeenCalledWith('playing')
    expect(globalStore.updateElapsed).toHaveBeenCalledTimes(1)
    expect(globalStore.updateElapsed).toHaveBeenCalledWith(0)
  })

  it('legacy song_added with non-empty queue does NOT infer advancement', () => {
    // Stateful assertion: existing current song. Legacy payload must NOT
    // shift current_index or change status — exhausted-queue advancement is
    // reserved for backend-authoritative state.
    liveQueueState.songs = [{ id: 'cur', title: 'Cur' }]
    liveQueueState.current_index = 0
    liveQueueState.current_song = { id: 'cur', title: 'Cur' }
    liveQueueState.status = 'playing'

    const song = { id: 'second', title: 'Second' }
    wsClient.handleMessage({
      type: 'song_added',
      data: {
        song,
        position: 1,
        activity: { type: 'song_added', user: 'Alice' },
      },
    })

    expect(globalStore.addSong).toHaveBeenCalledWith(song, 1)
    expect(globalStore.updateCurrentIndex).not.toHaveBeenCalled()
    expect(globalStore.updatePlaybackStatus).not.toHaveBeenCalled()
    expect(globalStore.updateElapsed).not.toHaveBeenCalled()
  })

  // --- Sprint 005 ---

  it('vote_updated updates the store and notifies vote event subscribers', () => {
    const callback = vi.fn()
    const unsubscribe = wsClient.onVoteEvent(callback)
    const session = {
      id: 'skip:song-1',
      created_at: '2026-06-20T10:00:00Z',
      voted_by: { 1: true },
    }
    const activity = {
      timestamp: '2026-06-20T10:00:01Z',
      description: 'Alice voted to skip "Song One" (1/2)',
    }

    wsClient.handleMessage({
      type: 'vote_updated',
      data: { session, activity },
    })

    expect(globalStore.upsertVoteSession).toHaveBeenCalledWith(session)
    expect(globalStore.addActivity).toHaveBeenCalledWith(activity)
    expect(callback).toHaveBeenCalledWith({
      type: 'vote_updated',
      data: { session, activity },
    })

    unsubscribe()
  })

  it('vote_updated without activity still upserts and notifies subscribers', () => {
    const callback = vi.fn()
    const unsubscribe = wsClient.onVoteEvent(callback)
    const session = {
      id: 'skip:song-1',
      created_at: '2026-06-20T10:00:00Z',
      voted_by: { 1: true },
    }

    expect(() => {
      wsClient.handleMessage({
        type: 'vote_updated',
        data: { session },
      })
    }).not.toThrow()

    expect(globalStore.upsertVoteSession).toHaveBeenCalledWith(session)
    expect(globalStore.addActivity).not.toHaveBeenCalled()
    expect(callback).toHaveBeenCalledWith({
      type: 'vote_updated',
      data: { session },
    })

    unsubscribe()
  })

  it('vote_resolved updates the store and notifies vote event subscribers', () => {
    const callback = vi.fn()
    const unsubscribe = wsClient.onVoteEvent(callback)
    const activity = {
      timestamp: '2026-06-20T10:00:02Z',
      description: 'vote to skip "Song One" passed',
    }

    wsClient.handleMessage({
      type: 'vote_resolved',
      data: {
        session_id: 'skip:song-1',
        outcome: 'passed',
        activity,
      },
    })

    expect(globalStore.removeVoteSession).toHaveBeenCalledWith('skip:song-1')
    expect(globalStore.addActivity).toHaveBeenCalledWith(activity)
    expect(callback).toHaveBeenCalledWith({
      type: 'vote_resolved',
      data: {
        session_id: 'skip:song-1',
        outcome: 'passed',
        activity,
      },
    })

    unsubscribe()
  })

  it('vote_resolved without activity still removes and notifies subscribers', () => {
    const consoleSpy = vi.spyOn(console, 'log').mockImplementation(() => {})
    const callback = vi.fn()
    const unsubscribe = wsClient.onVoteEvent(callback)

    expect(() => {
      wsClient.handleMessage({
        type: 'vote_resolved',
        data: {
          session_id: 'skip:song-1',
          outcome: 'expired',
        },
      })
    }).not.toThrow()

    expect(globalStore.removeVoteSession).toHaveBeenCalledWith('skip:song-1')
    expect(globalStore.addActivity).not.toHaveBeenCalled()
    expect(callback).toHaveBeenCalledWith({
      type: 'vote_resolved',
      data: {
        session_id: 'skip:song-1',
        outcome: 'expired',
      },
    })

    unsubscribe()
    consoleSpy.mockRestore()
  })

  it('isolates vote event subscriber errors from message handling and other subscribers', () => {
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const throwingCallback = vi.fn(() => {
      throw new Error('subscriber failed')
    })
    const healthyCallback = vi.fn()
    const unsubscribeThrowing = wsClient.onVoteEvent(throwingCallback)
    const unsubscribeHealthy = wsClient.onVoteEvent(healthyCallback)

    const session = {
      id: 'skip:song-1',
      created_at: '2026-06-20T10:00:00Z',
      voted_by: { 1: true },
    }
    wsClient.handleMessage({
      type: 'vote_updated',
      data: {
        session,
        activity: { description: 'Alice voted to skip "Song One" (1/2)' },
      },
    })

    expect(globalStore.upsertVoteSession).toHaveBeenCalledWith(session)
    expect(throwingCallback).toHaveBeenCalled()
    expect(healthyCallback).toHaveBeenCalled()
    expect(consoleSpy).toHaveBeenCalledWith('Error in voteEvent callback:', expect.any(Error))

    unsubscribeThrowing()
    unsubscribeHealthy()
    consoleSpy.mockRestore()
  })

  it('onVoteEvent returns an unsubscribe function', () => {
    const callback = vi.fn()
    const unsubscribe = wsClient.onVoteEvent(callback)
    unsubscribe()

    wsClient.handleMessage({
      type: 'vote_updated',
      data: {
        session: {
          id: 'skip:song-1',
          created_at: '2026-06-20T10:00:00Z',
          voted_by: { 1: true },
        },
        activity: { description: 'Alice voted to skip "Song One" (1/2)' },
      },
    })

    expect(callback).not.toHaveBeenCalled()
  })

  // --- Sequence gap / full-sync recovery ---

  function attachOpenSocket() {
    const fakeWs = {
      readyState: 1, // WebSocket.OPEN
      send: vi.fn(),
      close: vi.fn(),
    }
    wsClient.ws = fakeWs
    return fakeWs
  }

  it('gap detection sends request_full_sync when socket is open', () => {
    const fakeWs = attachOpenSocket()
    // First message establishes lastSeqNum.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 1 }, seq_num: 1 })
    // Skip seq_num 2 → gap detected.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 2 }, seq_num: 3 })

    expect(fakeWs.send).toHaveBeenCalledTimes(1)
    expect(fakeWs.send).toHaveBeenCalledWith(JSON.stringify({ type: 'request_full_sync' }))
  })

  it('repeated gaps while awaiting sync do not spam requests', () => {
    const fakeWs = attachOpenSocket()
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 1 }, seq_num: 1 })
    // Gap: seq 2 is missing, triggers request_full_sync.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 3 }, seq_num: 3 })
    // Another gap: seq 4 is missing, but pendingFullSync is still true.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 5 }, seq_num: 5 })
    // Yet another gap.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 7 }, seq_num: 7 })

    // Only one request_full_sync should have been sent.
    expect(fakeWs.send).toHaveBeenCalledTimes(1)
  })

  it('receiving full_sync applies queue state and clears the pending guard', () => {
    const fakeWs = attachOpenSocket()
    // Simulate a gap → request sent, pendingFullSync = true.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 1 }, seq_num: 1 })
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 3 }, seq_num: 3 })
    expect(fakeWs.send).toHaveBeenCalledTimes(1)
    expect(wsClient.pendingFullSync).toBe(true)

    // Server responds with full_sync.
    const state = { songs: [{ id: 'x', title: 'X' }], current_index: 0, status: 'playing', elapsed: 10 }
    wsClient.handleMessage({ type: 'full_sync', data: { state }, seq_num: 3 })

    expect(globalStore.updateQueueState).toHaveBeenCalledWith(state)
    expect(wsClient.pendingFullSync).toBe(false)
    // lastSeqNum retains full_sync's seq_num so subsequent gaps are detectable.
    expect(wsClient.lastSeqNum).toBe(3)

    // A subsequent gap (seq 3 → 5) now triggers a new request.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 5 }, seq_num: 5 })
    expect(fakeWs.send).toHaveBeenCalledTimes(2)
  })

  it('full_sync with a higher seq_num does not send request_full_sync', () => {
    const fakeWs = attachOpenSocket()
    // Client starts fresh (lastSeqNum = 0). Server sends full_sync with a
    // high seq_num (e.g. first message after connect).
    wsClient.handleMessage({
      type: 'full_sync',
      data: { state: { songs: [], current_index: -1, status: 'paused', elapsed: 0 } },
      seq_num: 42,
    })

    expect(fakeWs.send).not.toHaveBeenCalled()
    expect(wsClient.lastSeqNum).toBe(42)
  })

  it('after full_sync seq_num N, receiving delta seq_num N+1 does not request sync', () => {
    const fakeWs = attachOpenSocket()
    const N = 10
    wsClient.handleMessage({
      type: 'full_sync',
      data: { state: { songs: [], current_index: -1, status: 'paused', elapsed: 0 } },
      seq_num: N,
    })
    // First delta is consecutive: N → N+1.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 1 }, seq_num: N + 1 })

    expect(fakeWs.send).not.toHaveBeenCalled()
    expect(wsClient.lastSeqNum).toBe(N + 1)
  })

  it('after full_sync seq_num N, receiving delta seq_num N+2 does request sync', () => {
    const fakeWs = attachOpenSocket()
    const N = 10
    wsClient.handleMessage({
      type: 'full_sync',
      data: { state: { songs: [], current_index: -1, status: 'paused', elapsed: 0 } },
      seq_num: N,
    })
    // Delta skips N+1 → real gap: N → N+2.
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 2 }, seq_num: N + 2 })

    expect(fakeWs.send).toHaveBeenCalledTimes(1)
    expect(fakeWs.send).toHaveBeenCalledWith(JSON.stringify({ type: 'request_full_sync' }))
  })

  it('consecutive in-order seq_num messages do not request sync', () => {
    const fakeWs = attachOpenSocket()
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 1 }, seq_num: 1 })
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 2 }, seq_num: 2 })
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 3 }, seq_num: 3 })
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 4 }, seq_num: 4 })

    expect(fakeWs.send).not.toHaveBeenCalled()
    expect(wsClient.pendingFullSync).toBe(false)
  })

  // --- R05: error event recovery ---

  it('error event clears pendingFullSync so client is not stuck', () => {
    // Simulate a gap → request sent, pendingFullSync = true.
    const fakeWs = attachOpenSocket()
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 1 }, seq_num: 1 })
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 3 }, seq_num: 3 })
    expect(wsClient.pendingFullSync).toBe(true)

    const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})

    // Backend rejects with a structured error envelope (R05 additive event).
    wsClient.handleMessage({
      type: 'error',
      data: { code: 'unauthorized', message: 'session_token required to issue client requests' },
    })

    expect(wsClient.pendingFullSync).toBe(false)
    expect(consoleSpy).toHaveBeenCalled()
    const warningText = String(consoleSpy.mock.calls[0]?.[0] ?? '')
    // No raw token in the warning, ever.
    expect(warningText).not.toMatch(/tok-/)
    expect(warningText.toLowerCase()).toContain('websocket')

    // A subsequent gap must be able to request a new sync (not stuck).
    wsClient.handleMessage({ type: 'elapsed_sync', data: { elapsed: 5 }, seq_num: 5 })
    expect(fakeWs.send).toHaveBeenCalledTimes(2)

    consoleSpy.mockRestore()
  })

  // --- R05: session_token in connect URL ---

  it('connect URL includes session_token when sessionHelper.isValid()', async () => {
    // Arrange: a valid session in localStorage.
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession('tok-r05-url', future)

    // Stub WebSocket constructor so we can capture the URL.
    const wsInstances = []
    class FakeWS {
      constructor(url) {
        this.url = url
        this.readyState = 0
        this.close = vi.fn()
        this.send = vi.fn()
        wsInstances.push(this)
      }
    }
    vi.stubGlobal('WebSocket', FakeWS)
    // A01: legacy ?user_id= is no longer sent. Verify session_token is in the
    // URL and user_id is NOT.
    globalStore.currentUser = { id: 7 }

    try {
      wsClient.connect()
      expect(wsInstances).toHaveLength(1)
      const url = wsInstances[0].url
      expect(url).toContain('session_token=tok-r05-url')
      expect(url).not.toContain('user_id=')
    } finally {
      wsClient.disconnect()
      vi.unstubAllGlobals()
      sessionHelper.clearSession()
      globalStore.currentUser = null
    }
  })

  it('connect URL percent-encodes URL-sensitive characters in the session token', async () => {
    // Token containing characters that MUST be encoded in a query value:
    // '+', '/', '=', '?', '&', space, '%', '#'. A naive concatenation
    // (e.g. `?session_token=${token}`) would corrupt the URL or split the
    // query into multiple params. websocket.js routes through URLSearchParams
    // so the assertions here pin the encoding contract.
    const tricky = 'tok+with/sensitive=chars?&space %and#hash'
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession(tricky, future)

    const wsInstances = []
    class FakeWS {
      constructor(url) {
        this.url = url
        this.readyState = 0
        this.close = vi.fn()
        this.send = vi.fn()
        wsInstances.push(this)
      }
    }
    vi.stubGlobal('WebSocket', FakeWS)
    globalStore.currentUser = null

    try {
      wsClient.connect()
      expect(wsInstances).toHaveLength(1)
      const url = wsInstances[0].url

      // The raw token must NOT appear verbatim — every URL-sensitive
      // character must be percent-encoded (URLSearchParams uses `+` for
      // space per application/x-www-form-urlencoded).
      expect(url).not.toContain(tricky)
      expect(url).toContain('session_token=tok%2Bwith%2Fsensitive%3Dchars%3F%26space+%25and%23hash')
      // No accidental param splitting: only one query separator after /ws.
      expect(url.split('?')[1]?.split('&')).toHaveLength(1)
      // Decoded query parses back to the original token (decodeURIComponent
      // turns '+' back into space, matching form-encoding semantics).
      const qs = url.split('?')[1]
      const decoded = decodeURIComponent(qs.replace(/\+/g, ' '))
      expect(decoded).toBe(`session_token=${tricky}`)
    } finally {
      wsClient.disconnect()
      vi.unstubAllGlobals()
      sessionHelper.clearSession()
      globalStore.currentUser = null
    }
  })
})
