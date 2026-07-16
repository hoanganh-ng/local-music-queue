import { describe, it, expect, beforeEach, vi } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import { flushPromises } from '@vue/test-utils'
import { sessionHelper } from '../../services/session'
import DashboardView from '../DashboardView.vue'
import { globalStore } from '../../store'
import { wsClient } from '../../services/websocket'

const {
  mockToast,
  mockPush,
  wsMockState,
  voteNotificationsMock,
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
  voteNotificationsMock: {
    unsupported: { value: false },
    permission: { value: 'granted' },
    dismissed: { value: true },
    showCta: { value: false },
    canNotify: { value: true },
    requestPermission: vi.fn().mockResolvedValue('granted'),
    dismissCta: vi.fn(),
    notifyVote: vi.fn(),
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

vi.mock('../../composables/useVoteBrowserNotifications', () => ({
  useVoteBrowserNotifications: () => voteNotificationsMock,
}))

describe('DashboardView', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    globalStore.clearUser()
    wsMockState.songAddedCallback = null
    wsMockState.voteEventCallback = null
    // Reset composable mock to defaults before each test
    voteNotificationsMock.unsupported.value = false
    voteNotificationsMock.permission.value = 'granted'
    voteNotificationsMock.dismissed.value = true
    voteNotificationsMock.showCta.value = false
    voteNotificationsMock.canNotify.value = true
    voteNotificationsMock.requestPermission.mockClear()
    voteNotificationsMock.requestPermission.mockResolvedValue('granted')
    voteNotificationsMock.dismissCta.mockClear()
    voteNotificationsMock.notifyVote.mockClear()
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
    expect(voteNotificationsMock.notifyVote).toHaveBeenCalledWith(
      'vote_updated|skip:song-1|2026-06-20T10:00:00Z|1',
      'Vote update',
      'Alice voted to skip "Song One" (1/2)'
    )
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
    expect(voteNotificationsMock.notifyVote).not.toHaveBeenCalled()
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
    expect(voteNotificationsMock.notifyVote).toHaveBeenCalledWith(
      'vote_resolved|skip:song-1|passed|2026-06-20T10:00:01Z',
      'Vote passed',
      'vote to skip "Song One" passed'
    )
    expect(voteNotificationsMock.notifyVote).toHaveBeenCalledWith(
      'vote_resolved|skip:song-2|expired|2026-06-20T10:00:02Z',
      'Vote expired',
      'vote to skip "Song Two" expired'
    )
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

  it('renders QueueList in the left column and no SubmitForm', () => {
    const wrapper = shallowMount(DashboardView)
    const left = wrapper.find('.col-left')
    expect(left.exists()).toBe(true)
    expect(left.findComponent({ name: 'QueueList' }).exists()).toBe(true)
    expect(left.findComponent({ name: 'SubmitForm' }).exists()).toBe(false)
  })

  it('renders NowPlaying and SubmitForm in .col-center, SubmitForm after NowPlaying', () => {
    const wrapper = shallowMount(DashboardView)
    const center = wrapper.find('.col-center')
    expect(center.exists()).toBe(true)
    const kids = Array.from(center.element.children)
    expect(kids.length).toBeGreaterThanOrEqual(2)
    expect(kids[0].classList.contains('now-playing-card')).toBe(true)
    expect(kids[1].classList.contains('add-track-panel')).toBe(true)
    expect(center.findComponent({ name: 'NowPlaying' }).exists()).toBe(true)
    expect(center.findComponent({ name: 'SubmitForm' }).exists()).toBe(true)
  })

  it('keeps ActivityLog in the right column', () => {
    const wrapper = shallowMount(DashboardView)
    const right = wrapper.find('.col-right')
    expect(right.exists()).toBe(true)
    expect(right.findComponent({ name: 'ActivityLog' }).exists()).toBe(true)
  })

  it('truncates long descriptions in browser notification body to 117 chars + ellipsis', () => {
    shallowMount(DashboardView)

    const longDescription = 'A'.repeat(150)
    wsMockState.voteEventCallback({
      type: 'vote_updated',
      data: {
        session: {
          id: 'skip:song-1',
          created_at: '2026-06-20T10:00:00Z',
          voted_by: { 1: true },
        },
        activity: { description: longDescription },
      },
    })

    expect(mockToast.info).toHaveBeenCalledWith(longDescription)
    expect(voteNotificationsMock.notifyVote).toHaveBeenCalledWith(
      expect.any(String),
      'Vote update',
      'A'.repeat(117) + '...'
    )
  })

  it('hides the browser-notif CTA banner when unsupported', () => {
    voteNotificationsMock.unsupported.value = true
    voteNotificationsMock.showCta.value = false
    const wrapper = shallowMount(DashboardView)
    expect(wrapper.find('.browser-notif-cta').exists()).toBe(false)
  })

  it('hides the browser-notif CTA banner when permission is already granted', () => {
    voteNotificationsMock.permission.value = 'granted'
    voteNotificationsMock.showCta.value = false
    const wrapper = shallowMount(DashboardView)
    expect(wrapper.find('.browser-notif-cta').exists()).toBe(false)
  })

  it('hides the browser-notif CTA banner when permission is denied', () => {
    voteNotificationsMock.permission.value = 'denied'
    voteNotificationsMock.showCta.value = false
    const wrapper = shallowMount(DashboardView)
    expect(wrapper.find('.browser-notif-cta').exists()).toBe(false)
  })

  it('shows the CTA banner when permission is default and not dismissed', () => {
    voteNotificationsMock.permission.value = 'default'
    voteNotificationsMock.showCta.value = true
    const wrapper = shallowMount(DashboardView)
    const cta = wrapper.find('.browser-notif-cta')
    expect(cta.exists()).toBe(true)
    expect(cta.find('.cta-enable-btn').exists()).toBe(true)
    expect(cta.find('.cta-dismiss-btn').exists()).toBe(true)
  })

  it('CTA Enable button calls requestPermission', async () => {
    voteNotificationsMock.showCta.value = true
    const wrapper = shallowMount(DashboardView)
    const enableBtn = wrapper.find('.cta-enable-btn')
    expect(enableBtn.exists()).toBe(true)

    await enableBtn.trigger('click')

    expect(voteNotificationsMock.requestPermission).toHaveBeenCalledTimes(1)
  })

  it('CTA dismiss button calls dismissCta', async () => {
    voteNotificationsMock.showCta.value = true
    const wrapper = shallowMount(DashboardView)
    const dismissBtn = wrapper.find('.cta-dismiss-btn')

    await dismissBtn.trigger('click')

    expect(voteNotificationsMock.dismissCta).toHaveBeenCalledTimes(1)
  })

  describe('Copy Session Token for Extension', () => {
    let writeText

    beforeEach(() => {
      writeText = vi.fn().mockResolvedValue(undefined)
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText },
        configurable: true,
        writable: true,
      })
    })

    it('shows the copy button when sessionHelper.isValid() returns true', () => {
      const future = new Date(Date.now() + 60 * 60_000).toISOString()
      sessionHelper.saveSession('opaque-token-abc', future)

      const wrapper = shallowMount(DashboardView)
      const btn = wrapper.find('.copy-token-btn')

      expect(btn.exists()).toBe(true)
    })

    it('hides the copy button when sessionHelper.isValid() returns false', () => {
      // No session saved
      const wrapper = shallowMount(DashboardView)
      expect(wrapper.find('.copy-token-btn').exists()).toBe(false)
    })

    it('hides the copy button when session token is expired', () => {
      const past = new Date(Date.now() - 60_000).toISOString()
      sessionHelper.saveSession('expired', past)

      const wrapper = shallowMount(DashboardView)
      expect(wrapper.find('.copy-token-btn').exists()).toBe(false)
    })

    it('writes the current token to clipboard and toasts success on click', async () => {
      const future = new Date(Date.now() + 60 * 60_000).toISOString()
      const tokenValue = 'opaque-token-xyz'
      sessionHelper.saveSession(tokenValue, future)

      const wrapper = shallowMount(DashboardView)
      const btn = wrapper.find('.copy-token-btn')
      expect(btn.exists()).toBe(true)

      await btn.trigger('click')

      expect(writeText).toHaveBeenCalledTimes(1)
      expect(writeText).toHaveBeenCalledWith(tokenValue)
      expect(mockToast.success).toHaveBeenCalledWith('Session token copied. Paste it into the browser extension Options.')
      expect(mockToast.error).not.toHaveBeenCalled()
      expect(mockToast.info).not.toHaveBeenCalled()
    })

    it('toasts an info message and does not write to clipboard when no valid session exists', async () => {
      // sessionHelper.isValid() is false; copy button is hidden so we simulate
      // the defensive guard by calling the handler directly via the component vm.
      // The defensive guard must not throw and must not invoke the clipboard.
      const wrapper = shallowMount(DashboardView)
      expect(wrapper.find('.copy-token-btn').exists()).toBe(false)

      // Trigger the same code path the button would, by mounting and asserting the
      // guard contract: invoking when no valid session must not call clipboard.
      // We invoke the handler via the exposed vm method (added in Task 1 implementation).
      wrapper.vm.handleCopySessionToken?.()

      expect(writeText).not.toHaveBeenCalled()
      expect(mockToast.info).toHaveBeenCalledWith('No active session. Log in again to copy a fresh token for the extension.')
      expect(mockToast.success).not.toHaveBeenCalled()
      expect(mockToast.error).not.toHaveBeenCalled()
    })

    it('toasts an error and does not leak the token when clipboard write fails', async () => {
      const future = new Date(Date.now() + 60 * 60_000).toISOString()
      sessionHelper.saveSession('opaque-token-fail', future)
      writeText.mockRejectedValueOnce(new Error('clipboard blocked'))

      const wrapper = shallowMount(DashboardView)
      await wrapper.find('.copy-token-btn').trigger('click')

      // Allow the rejected promise's catch to run
      await flushPromises()

      expect(writeText).toHaveBeenCalledTimes(1)
      expect(mockToast.error).toHaveBeenCalledWith('Could not copy to clipboard. Use DevTools → Application → Local Storage → lmq_session_token as a fallback.')
      expect(mockToast.success).not.toHaveBeenCalled()
      // Token must not appear in any toast message
      const allToastCalls = [
        ...mockToast.success.mock.calls,
        ...mockToast.error.mock.calls,
        ...mockToast.info.mock.calls,
      ]
      allToastCalls.forEach(([msg]) => {
        expect(msg).not.toContain('opaque-token-fail')
      })
    })
  })

  // --- R05b1: Rooms navigation control ---

  describe('Rooms navigation (R05b1)', () => {
    it('renders a Rooms button in the top nav', () => {
      const wrapper = shallowMount(DashboardView)
      const btn = wrapper.find('[data-testid="rooms-nav-btn"]')
      expect(btn.exists()).toBe(true)
      expect(btn.text()).toMatch(/Rooms/i)
    })

    it('Rooms button navigates to the RoomEntry route', async () => {
      const wrapper = shallowMount(DashboardView)
      await wrapper.find('[data-testid="rooms-nav-btn"]').trigger('click')
      expect(mockPush).toHaveBeenCalledWith({ name: 'RoomEntry' })
    })
  })
})
