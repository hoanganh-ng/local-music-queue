import { describe, it, expect, beforeEach, vi } from 'vitest'
import { globalStore } from '../index'

describe('Global Store', () => {
  beforeEach(() => {
    // Reset the store state before each test
    globalStore.clearUser()
    globalStore.updateQueueState({
      status: 'stopped',
      current_song: null,
      queue: [],
      history: []
    })
    
    // Clear localStorage mock
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    })
  })

  it('should set and clear user', () => {
    const user = { name: 'Alice', role: 'Guest' }
    globalStore.setUser(user)
    expect(globalStore.currentUser).toEqual(user)

    globalStore.clearUser()
    expect(globalStore.currentUser).toBeNull()
  })

  it('should update queue state', () => {
    const newState = {
      status: 'playing',
      current_song: { title: 'Song 1' },
      queue: [{ title: 'Song 2' }]
    }
    globalStore.updateQueueState(newState)
    expect(globalStore.queueState.status).toBe('playing')
    expect(globalStore.queueState.current_song.title).toBe('Song 1')
    expect(globalStore.queueState.queue).toHaveLength(1)
  })

  it('should update playback status', () => {
    globalStore.updatePlaybackStatus('paused')
    expect(globalStore.queueState.status).toBe('paused')
  })
})
