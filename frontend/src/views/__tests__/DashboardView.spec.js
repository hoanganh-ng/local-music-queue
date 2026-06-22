import { describe, it, expect, beforeEach, vi } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import DashboardView from '../DashboardView.vue'
import { globalStore } from '../../store'
import { wsClient } from '../../services/websocket'

const {
  mockToast,
  mockPush,
  wsMockState,
} = vi.hoisted(() => ({
  mockToast: {
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
  },
  mockPush: vi.fn(),
  wsMockState: {
    songAddedCallback: null,
    voteEventCallback: null,
    unsubscribeSongAdded: vi.fn(),
    unsubscribeVoteEvent: vi.fn(),
  },
}))

vi.mock('../../services/api', () => ({
  api: {
    getQueue: vi.fn().mockResolvedValue({ songs: [], current_index: -1, status: 'idle' }),
    getAutoQueueStatus: vi.fn().mockResolvedValue({ enabled: false }),
  }
}))

vi.mock('../../services/websocket', () => ({
  wsClient: {
    connect: vi.fn(),
    disconnect: vi.fn(),
    onSongAdded: vi.fn((callback) => {
      wsMockState.songAddedCallback = callback
      return wsMockState.unsubscribeSongAdded
    }),
    onVoteEvent: vi.fn((callback) => {
      wsMockState.voteEventCallback = callback
      return wsMockState.unsubscribeVoteEvent
    }),
  }
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: mockPush,
  })
}))

vi.mock('../../composables/useToast', () => ({
  useToast: () => mockToast,
}))

describe('DashboardView', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    globalStore.clearUser()
    wsMockState.songAddedCallback = null
    wsMockState.voteEventCallback = null
    vi.clearAllMocks()
  })

  it('logout clears localStorage session, globalStore user, and navigates to Auth', async () => {
    globalStore.setUser({ id: 42, role: 'host', display_name: 'HostUser' })
    localStorage.setItem('lmq_session_token', 'active-token')
    localStorage.setItem('lmq_session_expires_at', 'expires-at')

    const wrapper = shallowMount(DashboardView)
    const logoutBtn = wrapper.find('.logout-btn')
    expect(logoutBtn.exists()).toBe(true)

    await logoutBtn.trigger('click')

    expect(localStorage.getItem('lmq_session_token')).toBeNull()
    expect(localStorage.getItem('lmq_session_expires_at')).toBeNull()
    expect(globalStore.currentUser).toBeNull()
    expect(wsClient.disconnect).toHaveBeenCalled()
    expect(mockPush).toHaveBeenCalledWith({ name: 'Auth' })
  })

  it('shows info toast for live vote updates', () => {
    shallowMount(DashboardView)

    wsMockState.voteEventCallback({
      type: 'vote_updated',
      data: {
        session: {
          id: 'skip:song-1',
          created_at: '2026-06-20T10:00:00Z',
          voted_by: { 1: true },
        },
        activity: {
          description: 'Alice voted to skip "Song One" (1/2)',
        },
      },
    })

    expect(mockToast.info).toHaveBeenCalledWith('Alice voted to skip "Song One" (1/2)')
  })

  it('suppresses initial-sync vote update notifications', () => {
    shallowMount(DashboardView)

    wsMockState.voteEventCallback({
      type: 'vote_updated',
      data: {
        initial_sync: true,
        session: {
          id: 'skip:song-1',
          created_at: '2026-06-20T10:00:00Z',
          voted_by: { 1: true },
        },
        activity: {
          description: 'Active vote session',
        },
      },
    })

    expect(mockToast.info).not.toHaveBeenCalled()
    expect(mockToast.success).not.toHaveBeenCalled()
  })

  it('ignores vote events without an activity description', () => {
    expect(() => {
      shallowMount(DashboardView)

      wsMockState.voteEventCallback({
        type: 'vote_updated',
        data: {
          session: {
            id: 'skip:song-1',
            created_at: '2026-06-20T10:00:00Z',
            voted_by: { 1: true },
          },
        },
      })
    }).not.toThrow()

    expect(mockToast.info).not.toHaveBeenCalled()
    expect(mockToast.success).not.toHaveBeenCalled()
    expect(mockToast.error).not.toHaveBeenCalled()
  })

  it('maps passed vote resolutions to success and expired resolutions to info', () => {
    shallowMount(DashboardView)

    wsMockState.voteEventCallback({
      type: 'vote_resolved',
      data: {
        session_id: 'skip:song-1',
        outcome: 'passed',
        activity: {
          timestamp: '2026-06-20T10:00:01Z',
          description: 'vote to skip "Song One" passed',
        },
      },
    })
    wsMockState.voteEventCallback({
      type: 'vote_resolved',
      data: {
        session_id: 'skip:song-2',
        outcome: 'expired',
        activity: {
          timestamp: '2026-06-20T10:00:02Z',
          description: 'vote to skip "Song Two" expired',
        },
      },
    })

    expect(mockToast.success).toHaveBeenCalledWith('vote to skip "Song One" passed')
    expect(mockToast.info).toHaveBeenCalledWith('vote to skip "Song Two" expired')
  })

  it('deduplicates vote updates and resolutions by their logical event keys', () => {
    shallowMount(DashboardView)

    const updateEvent = {
      type: 'vote_updated',
      data: {
        session: {
          id: 'skip:song-1',
          created_at: '2026-06-20T10:00:00Z',
          voted_by: { 1: true },
        },
        activity: {
          description: 'Alice voted to skip "Song One" (1/2)',
        },
      },
    }
    const resolutionEvent = {
      type: 'vote_resolved',
      data: {
        session_id: 'skip:song-1',
        outcome: 'expired',
        activity: {
          timestamp: '2026-06-20T10:00:30Z',
          description: 'vote to skip "Song One" expired',
        },
      },
    }

    wsMockState.voteEventCallback(updateEvent)
    wsMockState.voteEventCallback(updateEvent)
    wsMockState.voteEventCallback(resolutionEvent)
    wsMockState.voteEventCallback(resolutionEvent)

    expect(mockToast.info).toHaveBeenCalledTimes(2)
    expect(mockToast.info).toHaveBeenNthCalledWith(1, 'Alice voted to skip "Song One" (1/2)')
    expect(mockToast.info).toHaveBeenNthCalledWith(2, 'vote to skip "Song One" expired')
  })

  it('does not deduplicate same-song later vote sessions with a new creation time', () => {
    shallowMount(DashboardView)

    wsMockState.voteEventCallback({
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
    wsMockState.voteEventCallback({
      type: 'vote_updated',
      data: {
        session: {
          id: 'skip:song-1',
          created_at: '2026-06-20T10:05:00Z',
          voted_by: { 2: true },
        },
        activity: { description: 'Bob voted to skip "Song One" (1/2)' },
      },
    })

    expect(mockToast.info).toHaveBeenCalledTimes(2)
    expect(mockToast.info).toHaveBeenNthCalledWith(1, 'Alice voted to skip "Song One" (1/2)')
    expect(mockToast.info).toHaveBeenNthCalledWith(2, 'Bob voted to skip "Song One" (1/2)')
  })

  it('unsubscribes vote events on unmount', () => {
    const wrapper = shallowMount(DashboardView)

    wrapper.unmount()

    expect(wsMockState.unsubscribeSongAdded).toHaveBeenCalled()
    expect(wsMockState.unsubscribeVoteEvent).toHaveBeenCalled()
    expect(wsClient.disconnect).toHaveBeenCalled()
  })
})
