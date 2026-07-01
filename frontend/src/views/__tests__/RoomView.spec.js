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
  prioritizeRoomSong: vi.fn(),
  setRoomPlaybackStatus: vi.fn(),
  syncRoomPlayback: vi.fn(),
  skipRoomPlayback: vi.fn(),
  roomSongEnded: vi.fn(),
  getRoomAutoQueueStatus: vi.fn(),
  setRoomAutoQueueEnabled: vi.fn(),
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
    inst.onMessage({ type: 'room_queue_song_prioritized', data: { room_slug: 'lobby', from_index: 1, to_index: 1, song: { id: 'b', IsPrioritized: true }, state: { songs: [{ id: 'a' }, { id: 'b', IsPrioritized: true }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] } } })
    inst.onMessage({ type: 'room_queue_cleared', data: { room_slug: 'lobby', state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] } } })

    // An unrelated event MUST be ignored.
    inst.onMessage({ type: 'song_added', data: {} })
    inst.onMessage({ type: 'full_sync', data: {} })

    // The prioritized event landed — final state is the cleared state.
    expect(globalStore.roomQueues.lobby.state.songs).toEqual([])
    wrapper.unmount()
  })

  it('prioritizeSong calls api.prioritizeRoomSong with (slug, index)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.prioritizeRoomSong.mockResolvedValue(null)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()

    // Pull the live component instance and call its prioritizeSong.
    const vm = wrapper.vm
    await vm.prioritizeSong(2)
    expect(apiMock.prioritizeRoomSong).toHaveBeenCalledWith('lobby', 2)
    wrapper.unmount()
  })

  it('prioritizeSong surfaces 400/401/403/404/409 via toast without throwing', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const vm = wrapper.vm

    for (const status of [400, 401, 403, 404, 409]) {
      apiMock.prioritizeRoomSong.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status }))
      await vm.prioritizeSong(1)
      expect(toastMock.error).toHaveBeenCalled()
      toastMock.error.mockClear()
    }
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

  it('does NOT render global playback, vote, auto-queue, invite, or player-lease UI', () => {
    // R07d adds a room-scoped Prioritize button; "prioritize" is now
    // intentionally present in the rendered HTML. The other banned words
    // still must NOT appear — those are global playback controls that
    // belong in Dashboard, not in RoomView.
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper } = mountRoomView()
    const html = wrapper.html()
    for (const banned of ['radio-mode-toggle', 'volume', 'vote', 'invite', 'lease', 'skip', 'autoplay', 'auto-queue']) {
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

  // --- R09a playback tests ---

  it('handles room_playback_status_changed / elapsed_sync / song_advanced events', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance

    inst.onMessage({
      type: 'room_playback_status_changed',
      data: { room_slug: 'lobby', status: 'paused', elapsed: 5, state: { songs: [], current_index: -1, current_song: null, status: 'paused', elapsed: 5, queue: [], history: [] } }
    })
    expect(globalStore.roomQueues.lobby.state.status).toBe('paused')
    expect(globalStore.roomQueues.lobby.state.elapsed).toBe(5)

    inst.onMessage({
      type: 'room_playback_elapsed_sync',
      data: { room_slug: 'lobby', elapsed: 11, state: { songs: [], current_index: -1, current_song: null, status: 'paused', elapsed: 11, queue: [], history: [] } }
    })
    expect(globalStore.roomQueues.lobby.state.elapsed).toBe(11)

    inst.onMessage({
      type: 'room_playback_song_advanced',
      data: {
        room_slug: 'lobby',
        reason: 'skip',
        previous_index: 0,
        new_index: 1,
        current_song: { id: 'b' },
        status: 'playing',
        elapsed: 0,
        state: { songs: [{ id: 'a' }, { id: 'b' }], current_index: 1, current_song: { id: 'b' }, status: 'playing', elapsed: 0, queue: [], history: [] }
      }
    })
    expect(globalStore.roomQueues.lobby.state.current_index).toBe(1)
    expect(globalStore.roomQueues.lobby.state.current_song).toEqual({ id: 'b' })

    wrapper.unmount()
  })

  it('setPlaybackStatus / skipPlayback / songEnded call the matching api methods', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.setRoomPlaybackStatus.mockResolvedValue(null)
    apiMock.skipRoomPlayback.mockResolvedValue(null)
    apiMock.roomSongEnded.mockResolvedValue(null)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const vm = wrapper.vm

    await vm.setPlaybackStatus('playing')
    expect(apiMock.setRoomPlaybackStatus).toHaveBeenCalledWith('lobby', 'playing')

    await vm.skipPlayback()
    expect(apiMock.skipRoomPlayback).toHaveBeenCalledWith('lobby')

    await vm.songEnded()
    expect(apiMock.roomSongEnded).toHaveBeenCalledWith('lobby')

    wrapper.unmount()
  })

  it('playback toasts surface 400/401/403/404/409/410 without throwing', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const vm = wrapper.vm

    for (const status of [400, 401, 403, 404, 409, 410]) {
      apiMock.setRoomPlaybackStatus.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status }))
      await vm.setPlaybackStatus('paused')
      expect(toastMock.error).toHaveBeenCalled()
      toastMock.error.mockClear()
    }
    wrapper.unmount()
  })

  // --- R09g room auto-queue ---

  it('fetches room auto-queue status on mount and stores it in the per-room slice', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: true, strategy: 'related' })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    expect(apiMock.getRoomAutoQueueStatus).toHaveBeenCalledWith('lobby')
    expect(globalStore.roomQueues.lobby.autoQueueConfig).toEqual({ enabled: true, strategy: 'related' })
    // global autoQueueConfig MUST NOT be mutated.
    expect(globalStore.autoQueueConfig).toEqual({ enabled: false, strategy: 'related' })
    wrapper.unmount()
  })

  it('re-fetches room auto-queue status when the room slug changes', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    apiMock.getRoomAutoQueueStatus.mockClear()
    apiMock.getRoomAutoQueueStatus.mockResolvedValueOnce({ enabled: true, strategy: 'related' })
    await router.push('/rooms/lounge')
    await flushPromises()
    expect(apiMock.getRoomAutoQueueStatus).toHaveBeenCalledWith('lounge')
    expect(globalStore.roomQueues.lounge.autoQueueConfig).toEqual({ enabled: true, strategy: 'related' })
    wrapper.unmount()
  })

  it('toggleRoomAutoQueue calls api.setRoomAutoQueueEnabled with the flipped value and stores the response', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.setRoomAutoQueueEnabled.mockResolvedValue({ enabled: true, strategy: 'related' })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // Simulate an authenticated, connected user so the toggle is enabled.
    globalStore.setUser({ id: 'u1', display_name: 'Host', user_role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    await vm.toggleRoomAutoQueue()
    expect(apiMock.setRoomAutoQueueEnabled).toHaveBeenCalledWith('lobby', true)
    expect(globalStore.roomQueues.lobby.autoQueueConfig).toEqual({ enabled: true, strategy: 'related' })
    wrapper.unmount()
  })

  it('toggleRoomAutoQueue surfaces 401/403/404/409 via toast without throwing', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // Simulate an authenticated, connected user so the toggle is enabled.
    globalStore.setUser({ id: 'u1', display_name: 'Host', user_role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    for (const status of [401, 403, 404, 409]) {
      apiMock.setRoomAutoQueueEnabled.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status }))
      await vm.toggleRoomAutoQueue()
      expect(toastMock.error).toHaveBeenCalled()
      toastMock.error.mockClear()
    }
    wrapper.unmount()
  })

  it('handles room_auto_queue_added and room_auto_queue_config_changed events', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    inst.onMessage({
      type: 'room_auto_queue_added',
      data: { room_slug: 'lobby', song: { id: 'auto', added_by: 'system:autoqueue' }, source_song_title: 'src', current_index: 1, current_song: { id: 'auto' }, status: 'playing', elapsed: 0, state: { songs: [{ id: 'src' }, { id: 'auto' }], current_index: 1, current_song: { id: 'auto' }, status: 'playing', elapsed: 0, queue: [], history: [] } }
    })
    expect(globalStore.roomQueues.lobby.state.songs[1].id).toBe('auto')
    inst.onMessage({ type: 'room_auto_queue_config_changed', data: { room_slug: 'lobby', enabled: true, strategy: 'related' } })
    expect(globalStore.roomQueues.lobby.autoQueueConfig).toEqual({ enabled: true, strategy: 'related' })
    // global autoQueueConfig MUST remain unchanged.
    expect(globalStore.autoQueueConfig).toEqual({ enabled: false, strategy: 'related' })
    wrapper.unmount()
  })
})
