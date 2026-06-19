import { describe, it, expect, vi, beforeEach } from 'vitest'
import { wsClient } from '../websocket'
import { globalStore } from '../../store'

// Mock the globalStore with the surface websocket.js touches.
vi.mock('../../store', () => ({
  globalStore: {
    queueState: { songs: [], status: 'paused', current_index: -1 },
    updateQueueState: vi.fn(),
    addSong: vi.fn(),
    addActivity: vi.fn(),
    updateCurrentIndex: vi.fn(),
    updatePlaybackStatus: vi.fn(),
    updateElapsed: vi.fn(),
  }
}))

describe('WebSocketClient', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    globalStore.queueState = { songs: [], status: 'paused', current_index: -1 }
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

  it('song_added without authoritative fields skips authoritative setters', () => {
    wsClient.handleMessage({
      type: 'song_added',
      data: {
        song: { id: 'x', title: 'X' },
        position: 0,
        activity: { type: 'song_added', user: 'Alice' },
      },
    })

    expect(globalStore.addSong).toHaveBeenCalled()
    expect(globalStore.updateCurrentIndex).not.toHaveBeenCalled()
    expect(globalStore.updatePlaybackStatus).not.toHaveBeenCalled()
    expect(globalStore.updateElapsed).not.toHaveBeenCalled()
  })
})
