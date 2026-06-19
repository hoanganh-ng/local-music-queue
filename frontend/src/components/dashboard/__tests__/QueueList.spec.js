import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import QueueList from '../QueueList.vue'
import { globalStore } from '../../../store'

const mockPush = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: mockPush,
  })
}))

const mockRemoveSong = vi.fn()
vi.mock('../../../services/api', () => ({
  api: {
    removeSong: (...args) => mockRemoveSong(...args),
    prioritizeSong: vi.fn(),
  }
}))

describe('QueueList', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    globalStore.clearUser()
    mockPush.mockReset()
    mockRemoveSong.mockReset()
    vi.restoreAllMocks()
  })

  it('renders "Queue is empty" when queue is empty', () => {
    const wrapper = mount(QueueList, {
      props: {
        queue: []
      }
    })
    expect(wrapper.text()).toContain('Queue is empty.')
  })

  it('renders a list of songs when queue has items', () => {
    const mockQueue = [
      { id: 1, title: 'Song 1', added_by: 'Alice' },
      { id: 2, title: 'Song 2', added_by: 'Bob' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue
      }
    })
    
    const items = wrapper.findAll('.queue-item')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('Song 1')
    expect(items[0].text()).toContain('Added by Alice')
    expect(items[1].text()).toContain('Song 2')
    expect(items[1].text()).toContain('Added by Bob')
  })

  it('shows the correct song count in the header', () => {
    const mockQueue = [
      { id: 1, title: 'Song 1', added_by: 'Alice' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue
      }
    })
    
    expect(wrapper.find('.queue-count').text()).toBe('1 songs')
  })

  it('shows remove button for Guest on their own song', () => {
    globalStore.setUser({ id: 10, role: 'guest', display_name: 'Guest1' })
    const mockQueue = [
      { id: 101, title: 'Song 1', added_by_id: 10, added_by: 'Guest1' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue,
        currentIndex: 0,
        canControl: false
      }
    })

    expect(wrapper.find('.remove-btn').exists()).toBe(true)
  })

  it('does not show remove button for Guest on another user\'s song', () => {
    globalStore.setUser({ id: 10, role: 'guest', display_name: 'Guest1' })
    const mockQueue = [
      { id: 102, title: 'Song 2', added_by_id: 20, added_by: 'Guest2' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue,
        currentIndex: 0,
        canControl: false
      }
    })

    expect(wrapper.find('.remove-btn').exists()).toBe(false)
  })

  it('does not show remove button for Guest on AddedByID == 0 song', () => {
    globalStore.setUser({ id: 10, role: 'guest', display_name: 'Guest1' })
    const mockQueue = [
      { id: 103, title: 'Song 3', added_by_id: 0, added_by: 'system:autoqueue' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue,
        currentIndex: 0,
        canControl: false
      }
    })

    expect(wrapper.find('.remove-btn').exists()).toBe(false)
  })

  it('shows remove button for Host on any song', () => {
    globalStore.setUser({ id: 1, role: 'host', display_name: 'HostUser' })
    const mockQueue = [
      { id: 102, title: 'Song 2', added_by_id: 20, added_by: 'Guest2' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue,
        currentIndex: 0,
        canControl: true
      }
    })

    expect(wrapper.find('.remove-btn').exists()).toBe(true)
  })

  it('shows remove button for Admin on any song', () => {
    globalStore.setUser({ id: 2, role: 'admin', display_name: 'AdminUser' })
    const mockQueue = [
      { id: 102, title: 'Song 2', added_by_id: 20, added_by: 'Guest2' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue,
        currentIndex: 0,
        canControl: true
      }
    })

    expect(wrapper.find('.remove-btn').exists()).toBe(true)
  })

  it('remove click uses currentIndex + 1 + upNextIndex', async () => {
    globalStore.setUser({ id: 10, role: 'guest', display_name: 'Guest1' })
    const mockQueue = [
      { id: 101, title: 'Song 1', added_by_id: 10, added_by: 'Guest1' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue,
        currentIndex: 5,
        canControl: false
      }
    })

    const removeBtn = wrapper.find('.remove-btn')
    expect(removeBtn.exists()).toBe(true)

    mockRemoveSong.mockResolvedValueOnce(null)

    await removeBtn.trigger('click')

    expect(mockRemoveSong).toHaveBeenCalledWith(6, 'Guest1')
  })

  it('401 clears session/user and navigates to Auth', async () => {
    globalStore.setUser({ id: 10, role: 'guest', display_name: 'Guest1' })
    sessionStorage.setItem('lmq_session_token', 'test-token')

    const mockQueue = [
      { id: 101, title: 'Song 1', added_by_id: 10, added_by: 'Guest1' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue,
        currentIndex: 0,
        canControl: false
      }
    })

    const err = new Error('Unauthorized')
    err.status = 401
    mockRemoveSong.mockRejectedValueOnce(err)

    await wrapper.find('.remove-btn').trigger('click')

    expect(globalStore.currentUser).toBeNull()
    expect(sessionStorage.getItem('lmq_session_token')).toBeNull()
    expect(mockPush).toHaveBeenCalledWith({ name: 'Auth' })
  })

  it('403 shows permission feedback without clearing the session', async () => {
    globalStore.setUser({ id: 10, role: 'guest', display_name: 'Guest1' })
    sessionStorage.setItem('lmq_session_token', 'test-token')

    const mockQueue = [
      { id: 101, title: 'Song 1', added_by_id: 10, added_by: 'Guest1' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue,
        currentIndex: 0,
        canControl: false
      }
    })

    const err = new Error('Forbidden')
    err.status = 403
    mockRemoveSong.mockRejectedValueOnce(err)

    await wrapper.find('.remove-btn').trigger('click')

    expect(globalStore.currentUser).not.toBeNull()
    expect(sessionStorage.getItem('lmq_session_token')).toBe('test-token')
    expect(mockPush).not.toHaveBeenCalled()
  })

  it('successful removal does not mutate queue state optimistically', async () => {
    globalStore.setUser({ id: 10, role: 'guest', display_name: 'Guest1' })
    const initialQueue = [
      { id: 101, title: 'Song 1', added_by_id: 10, added_by: 'Guest1' }
    ]
    globalStore.updateQueueState({ queue: initialQueue, current_index: 0 })

    const wrapper = mount(QueueList, {
      props: {
        queue: initialQueue,
        currentIndex: 0,
        canControl: false
      }
    })

    mockRemoveSong.mockResolvedValueOnce(null)

    await wrapper.find('.remove-btn').trigger('click')

    expect(globalStore.queueState.queue).toHaveLength(1)
    expect(globalStore.queueState.queue[0].id).toBe(101)
  })
})
