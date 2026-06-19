import { describe, it, expect, beforeEach, vi } from 'vitest'
import { shallowMount } from '@vue/test-utils'
import DashboardView from '../DashboardView.vue'
import { globalStore } from '../../store'
import { wsClient } from '../../services/websocket'

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
    onSongAdded: vi.fn().mockReturnValue(vi.fn()),
  }
}))

const { mockPush } = vi.hoisted(() => ({
  mockPush: vi.fn()
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: mockPush,
  })
}))

describe('DashboardView', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    globalStore.clearUser()
    vi.clearAllMocks()
  })

  it('logout clears sessionStorage, globalStore user, and navigates to Auth', async () => {
    globalStore.setUser({ id: 42, role: 'host', display_name: 'HostUser' })
    sessionStorage.setItem('lmq_session_token', 'active-token')
    sessionStorage.setItem('lmq_session_expires_at', 'expires-at')

    const wrapper = shallowMount(DashboardView)
    const logoutBtn = wrapper.find('.logout-btn')
    expect(logoutBtn.exists()).toBe(true)

    await logoutBtn.trigger('click')

    expect(sessionStorage.getItem('lmq_session_token')).toBeNull()
    expect(sessionStorage.getItem('lmq_session_expires_at')).toBeNull()
    expect(globalStore.currentUser).toBeNull()
    expect(wsClient.disconnect).toHaveBeenCalled()
    expect(mockPush).toHaveBeenCalledWith({ name: 'Auth' })
  })
})
