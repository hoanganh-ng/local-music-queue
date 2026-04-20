import { describe, it, expect, vi, beforeEach } from 'vitest'
import { wsClient } from '../websocket'
import { globalStore } from '../../store'

// Mock the globalStore
vi.mock('../../store', () => ({
  globalStore: {
    updateQueueState: vi.fn()
  }
}))

describe('WebSocketClient', () => {
  beforeEach(() => {
    vi.clearAllMocks()
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
})
