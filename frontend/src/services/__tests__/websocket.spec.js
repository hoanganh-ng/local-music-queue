import { describe, it, expect, vi, beforeEach } from 'vitest'
import { wsClient } from '../websocket'
import { globalStore } from '../../store'

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
})
