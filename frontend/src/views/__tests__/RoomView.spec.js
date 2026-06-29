import { describe, it, expect, beforeEach, vi } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { globalStore } from '../../store'
import { sessionHelper } from '../../services/session'

// Capture the latest room-ws-client factory args / fake instance for the test.
const wsFactoryMock = vi.hoisted(() => {
  return {
    lastInstance: null,
    lastOptions: null,
    instances: [],
    makeClient() {
      const inst = {
        connect: vi.fn(),
        disconnect: vi.fn(),
        isOpen: vi.fn(() => true),
        send: vi.fn(() => false),
        lastSeqNum: 0,
        hasPriorSeq: false,
        onMessage: null,
        onGap: null,
        onOpen: null,
        onClose: null,
        onError: null,
      }
      wsFactoryMock.lastInstance = inst
      wsFactoryMock.instances.push(inst)
      return inst
    },
  }
})

vi.mock('../../services/room-websocket', () => ({
  createRoomWsClient: (slug, options) => {
    const inst = wsFactoryMock.makeClient()
    wsFactoryMock.lastOptions = options
    return inst
  },
}))

const apiMock = vi.hoisted(() => ({
  getRoomQueue: vi.fn(),
  addRoomSong: vi.fn(),
  removeRoomSong: vi.fn(),
  clearRoomQueue: vi.fn(),
}))
vi.mock('../../services/api', () => ({ api: apiMock }))

const toastMock = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn(), info: vi.fn() }))
vi.mock('../../composables/useToast', () => ({ useToast: () => toastMock }))

import RoomView from '../RoomView.vue'

function mountRoomView(slug = 'lobby', routePath = `/rooms/${slug}`) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/rooms/:slug', name: 'Room', component: RoomView, meta: { requiresAuth: true } },
      { path: '/', name: 'Dashboard', component: { template: '<div />' } },
    ],
  })
  return { router, wrapper: shallowMount(RoomView, { global: { plugins: [router] } }) }
}

describe('RoomView', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.roomQueues = {}
    vi.clearAllMocks()
  })

  it('subscribes the room ws client to the exact event types and ignores others', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance

    // Trigger each event the view MUST handle.
    inst.onMessage({ type: 'room_queue_sync', data: { room_slug: 'lobby', state: { songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] } } })
    inst.onMessage({ type: 'room_queue_song_added', data: { room_slug: 'lobby', song: { id: 'b' }, position: 1, state: { songs: [{ id: 'a' }, { id: 'b' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] } } })
    inst.onMessage({ type: 'room_queue_song_removed', data: { room_slug: 'lobby', removed_index: 0, state: { songs: [{ id: 'b' }], current_index: 0, current_song: { id: 'b' }, status: 'paused', queue: [], history: [] } } })
    inst.onMessage({ type: 'room_queue_cleared', data: { room_slug: 'lobby', state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] } } })

    // An unrelated event MUST be ignored.
    inst.onMessage({ type: 'song_added', data: {} })
    inst.onMessage({ type: 'full_sync', data: {} })

    expect(globalStore.roomQueues.lobby.state.songs).toEqual([])
    wrapper.unmount()
  })

  it('onGap triggers a REST refetch via api.getRoomQueue and applies it', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [{ id: 'restored' }], current_index: 0, current_song: { id: 'restored' }, status: 'paused', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    apiMock.getRoomQueue.mockClear()

    const inst = wsFactoryMock.lastInstance
    // Invoke the captured onGap.
    await inst.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })
    await flushPromises()

    expect(apiMock.getRoomQueue).toHaveBeenCalledWith('lobby')
    expect(globalStore.roomQueues.lobby.state.songs[0].id).toBe('restored')
    wrapper.unmount()
  })

  it('on REST error during recovery, captures lastError and does not throw', async () => {
    // The route-param watcher (R07c polish) REST-seeds the new room on
    // navigation; queue one rejection for that seed and one for the onGap
    // refetch below so both rejections are accounted for.
    apiMock.getRoomQueue.mockRejectedValueOnce(Object.assign(new Error('forbidden'), { status: 403 }))
    apiMock.getRoomQueue.mockRejectedValueOnce(Object.assign(new Error('forbidden'), { status: 403 }))
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    apiMock.getRoomQueue.mockClear()

    const inst = wsFactoryMock.lastInstance
    await inst.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })
    await flushPromises()

    expect(globalStore.roomQueues.lobby.lastError).toMatch(/403|forbidden/i)
    expect(globalStore.roomQueues.lobby.recoveryInFlight).toBe(false)
    wrapper.unmount()
  })

  it('disconnect is invoked on unmount and per-slug state is cleared', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    expect(globalStore.roomQueues.lobby).toBeTruthy()
    const inst = wsFactoryMock.lastInstance
    wrapper.unmount()
    expect(inst.disconnect).toHaveBeenCalled()
    expect(globalStore.roomQueues.lobby).toBeUndefined()
  })

  it('401/403/404/409 from REST are surfaced as a clear per-status message', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    apiMock.getRoomQueue.mockClear()

    for (const status of [401, 403, 404, 409]) {
      apiMock.getRoomQueue.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status }))
      const inst = wsFactoryMock.lastInstance
      await inst.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })
      await flushPromises()
      expect(toastMock.error).toHaveBeenCalled()
      // Reset call counts between assertions.
      toastMock.error.mockClear()
    }
    wrapper.unmount()
  })

  it('does NOT render global playback, vote, priority, auto-queue, invite, or player-lease UI', () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper } = mountRoomView()
    const html = wrapper.html()
    for (const banned of ['radio-mode-toggle', 'volume', 'vote', 'prioritize', 'invite', 'lease', 'skip', 'autoplay', 'auto-queue']) {
      expect(html.toLowerCase()).not.toContain(banned)
    }
  })

  it('route-param change REST-seeds the new room and rebuilds the ws client', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()

    const firstClient = wsFactoryMock.lastInstance
    expect(apiMock.getRoomQueue).toHaveBeenCalledWith('lobby')

    // Change the route param: lounge is a different room.
    apiMock.getRoomQueue.mockClear()
    await router.push('/rooms/lounge')
    await flushPromises()

    // REST-seed must have been called for the NEW slug before the new WS opens.
    expect(apiMock.getRoomQueue).toHaveBeenCalledWith('lounge')
    expect(firstClient.disconnect).toHaveBeenCalled()
    const secondClient = wsFactoryMock.lastInstance
    expect(secondClient).not.toBe(firstClient)
    expect(secondClient.connect).toHaveBeenCalled()
    expect(globalStore.roomQueues.lounge).toBeTruthy()
    wrapper.unmount()
  })

  it('does not send raw token / raw URL in toast or log payloads on errors', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    apiMock.getRoomQueue.mockClear()

    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {})
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    const inst = wsFactoryMock.lastInstance
    apiMock.getRoomQueue.mockRejectedValueOnce(Object.assign(new Error('forbidden'), { status: 403 }))
    await inst.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })
    await flushPromises()

    for (const spy of [logSpy, errSpy]) {
      for (const call of spy.mock.calls) {
        const flat = call.map(a => typeof a === 'string' ? a : JSON.stringify(a)).join(' ')
        expect(flat).not.toContain(/session_token=/)
        expect(flat).not.toContain(/ws\/rooms\/lobby/)
      }
    }
    for (const call of toastMock.error.mock.calls) {
      const flat = call.map(a => typeof a === 'string' ? a : JSON.stringify(a)).join(' ')
      expect(flat).not.toContain(/session_token=/)
      expect(flat).not.toContain(/ws\/rooms\/lobby/)
    }

    logSpy.mockRestore()
    errSpy.mockRestore()
    wrapper.unmount()
  })
})
