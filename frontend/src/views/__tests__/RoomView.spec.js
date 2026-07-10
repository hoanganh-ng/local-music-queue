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
  // R10c
  deleteRoom: vi.fn(),
  removeRoomMember: vi.fn(),
  getRoomMembers: vi.fn(),
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

  // --- R10c room deletion + member removal ---

  it('host-panel is hidden for non-host users', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 'u2', display_name: 'Guest', role: 'guest' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const html = wrapper.html()
    expect(html).not.toContain('host-panel')
    expect(html).not.toContain('archive-room-btn')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('host-panel is hidden for admin users (host-only is per R10a/R10c)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 'u3', display_name: 'Admin', role: 'admin' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const html = wrapper.html()
    expect(html).not.toContain('host-panel')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('host-panel is visible to the host when connected', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 'u1', display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.applyRoomMembersChanged('lobby', {
      members: [
        { user_id: 1, role: 'host' },
        { user_id: 2, role: 'guest' },
        { user_id: 3, role: 'admin' },
      ],
    })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // Re-assert connected (mount path resets it; mock ws.onopen never fires).
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const html = wrapper.html()
    expect(html).toContain('host-panel')
    expect(html).toContain('archive-room-btn')
    // Host row entry MUST NOT show a Remove button (cannot remove host).
    // Self entry MUST NOT show a Remove button (cannot remove self).
    // The guest entry MUST show a Remove button.
    expect(html).toContain('remove-member-2')
    expect(html).not.toContain('remove-member-1')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('host-panel is hidden when the room is archived (archived banner is shown)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 'u1', display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.markRoomArchived('lobby')
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const html = wrapper.html()
    expect(html).not.toContain('host-panel')
    expect(html).toContain('archived-banner')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('host-panel is hidden when the current viewer was removed (removed banner is shown)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 'u1', display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.markRoomRemovedAsCurrentUser('lobby')
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const html = wrapper.html()
    expect(html).not.toContain('host-panel')
    expect(html).toContain('removed-banner')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('deleteRoom calls api.deleteRoom with the slug and flips archived=true on 204', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.deleteRoom.mockResolvedValue(null)
    globalStore.setUser({ id: 'u1', display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    // Bypass the window.confirm() prompt.
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    await vm.deleteRoom()
    expect(apiMock.deleteRoom).toHaveBeenCalledWith('lobby')
    expect(globalStore.roomQueues.lobby.archived).toBe(true)
    confirmSpy.mockRestore()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('deleteRoom aborts cleanly when the user cancels the confirmation', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.deleteRoom.mockResolvedValue(null)
    globalStore.setUser({ id: 'u1', display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    await vm.deleteRoom()
    expect(apiMock.deleteRoom).not.toHaveBeenCalled()
    expect(globalStore.roomQueues.lobby.archived).toBe(false)
    confirmSpy.mockRestore()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('deleteRoom surfaces 400/401/403/404/409 via toast without throwing', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 'u1', display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    for (const status of [400, 401, 403, 404, 409]) {
      apiMock.deleteRoom.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status }))
      await vm.deleteRoom()
      expect(toastMock.error).toHaveBeenCalled()
      toastMock.error.mockClear()
    }
    confirmSpy.mockRestore()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('removeRoomMember calls api.removeRoomMember with (slug, userId)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.removeRoomMember.mockResolvedValue(null)
    globalStore.setUser({ id: 'u1', display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.applyRoomMembersChanged('lobby', {
      members: [
        { user_id: 1, role: 'host' },
        { user_id: 2, role: 'guest' },
      ],
    })
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    await vm.removeRoomMember({ user_id: 2, role: 'guest' })
    expect(apiMock.removeRoomMember).toHaveBeenCalledWith('lobby', 2)
    confirmSpy.mockRestore()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('removeRoomMember rejects host and self targets locally without calling the API', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.applyRoomMembersChanged('lobby', {
      members: [
        { user_id: 1, role: 'host' },
        { user_id: 1, role: 'host' }, // duplicate for self-match tests
      ],
    })
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    // Host target
    await vm.removeRoomMember({ user_id: 1, role: 'host' })
    expect(apiMock.removeRoomMember).not.toHaveBeenCalled()
    confirmSpy.mockRestore()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('removeRoomMember surfaces 400/401/403/404/409 via toast without throwing', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.applyRoomMembersChanged('lobby', {
      members: [{ user_id: 1, role: 'host' }, { user_id: 2, role: 'guest' }],
    })
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    for (const status of [400, 401, 403, 404, 409]) {
      apiMock.removeRoomMember.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status }))
      await vm.removeRoomMember({ user_id: 2, role: 'guest' })
      expect(toastMock.error).toHaveBeenCalled()
      toastMock.error.mockClear()
    }
    confirmSpy.mockRestore()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('handles room_archived event by flipping archived=true and rendering the archived banner', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 2, display_name: 'Guest', role: 'guest' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // Re-assert connected because RoomView's mount path resets it to false
    // before the mock ws.onopen fires (the test mock never calls onOpen).
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    inst.onMessage({ type: 'room_archived', data: { room_id: 1, reason: 'host_archived', archived_at: '2026-07-10T00:00:00Z' } })
    await flushPromises()
    expect(globalStore.roomQueues.lobby.archived).toBe(true)
    const html = wrapper.html()
    expect(html).toContain('archived-banner')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('handles room_member_removed event for the current viewer by flipping removed=true', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 42, display_name: 'Me', role: 'guest' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    inst.onMessage({ type: 'room_member_removed', data: { room_slug: 'lobby', user_id: 42, reason: 'host_removed' } })
    await flushPromises()
    expect(globalStore.roomQueues.lobby.removed).toBe(true)
    const html = wrapper.html()
    expect(html).toContain('removed-banner')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('handles room_member_removed for a NON-current viewer without flipping removed=true', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'guest' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    inst.onMessage({ type: 'room_member_removed', data: { room_slug: 'lobby', user_id: 99, reason: 'host_removed' } })
    expect(globalStore.roomQueues.lobby.removed).toBe(false)
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('handles room_members_changed by replacing the per-room members list', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'guest' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    inst.onMessage({
      type: 'room_members_changed',
      data: {
        room_slug: 'lobby',
        members: [
          { user_id: 1, role: 'host' },
          { user_id: 2, role: 'admin' },
          { user_id: 3, role: 'guest' },
        ],
      },
    })
    expect(globalStore.roomQueues.lobby.members).toEqual([
      { user_id: 1, role: 'host' },
      { user_id: 2, role: 'admin' },
      { user_id: 3, role: 'guest' },
    ])
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('room_archived event disables room controls via the canMutate gate', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 2, display_name: 'Guest', role: 'guest' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // Re-assert connected (mount path resets to false; mock never calls onOpen).
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    expect(wrapper.vm.canMutate).toBe(true)
    const inst = wsFactoryMock.lastInstance
    inst.onMessage({ type: 'room_archived', data: { room_id: 1, reason: 'host_archived', archived_at: '2026-07-10T00:00:00Z' } })
    await flushPromises()
    expect(wrapper.vm.canMutate).toBe(false)
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('onClose for the per-room ws only flips connected=false (does NOT silently reconnect)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 2, display_name: 'Guest', role: 'guest' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    // Benign close (code 1006 — abnormal closure, e.g. network blip)
    // must only flip connected=false. No silent reconnect, no removal
    // flag flip.
    inst.onClose({ code: 1006, reason: '', wasClean: false })
    expect(globalStore.roomQueues.lobby.connected).toBe(false)
    expect(globalStore.roomQueues.lobby.removed).toBe(false)
    // No automatic connect() call should happen on a benign close.
    // (The remove flow only sends a 1008 — the RoomView does not
    // retry; the user must re-navigate.)
    expect(inst.connect).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    globalStore.clearUser()
  })

  // --- R10c member-list initial fetch ---

  it('fetches room members on mount and stores the initial list', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({
      members: [
        { user_id: 1, role: 'host' },
        { user_id: 2, role: 'guest' },
      ],
    })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    expect(apiMock.getRoomMembers).toHaveBeenCalledWith('lobby')
    expect(globalStore.roomQueues.lobby.members).toEqual([
      { user_id: 1, role: 'host' },
      { user_id: 2, role: 'guest' },
    ])
    wrapper.unmount()
  })

  it('re-fetches room members when the room slug changes', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [{ user_id: 1, role: 'host' }] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    apiMock.getRoomMembers.mockClear()
    apiMock.getRoomMembers.mockResolvedValue({ members: [{ user_id: 5, role: 'host' }] })
    await router.push('/rooms/lounge')
    await flushPromises()
    expect(apiMock.getRoomMembers).toHaveBeenCalledWith('lounge')
    expect(globalStore.roomQueues.lounge.members).toEqual([{ user_id: 5, role: 'host' }])
    wrapper.unmount()
  })

  it('initial members fetch failure surfaces 401/403/404/409 via toast without throwing', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    for (const status of [401, 403, 404, 409]) {
      apiMock.getRoomMembers.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status }))
      const { wrapper, router } = mountRoomView()
      await router.push('/rooms/lobby')
      await flushPromises()
      expect(toastMock.error).toHaveBeenCalled()
      toastMock.error.mockClear()
      wrapper.unmount()
    }
  })

  it('host can perform the first removal after the initial members fetch (no room_members_changed before)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({
      members: [
        { user_id: 1, role: 'host' },
        { user_id: 2, role: 'guest' },
      ],
    })
    apiMock.removeRoomMember.mockResolvedValue(null)
    globalStore.setUser({ id: 1, display_name: 'Host', role: 'host' })
    globalStore.setRoomQueueConnected('lobby', true)
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    // No room_members_changed event has fired yet — the host panel
    // must still be able to remove member 2 because the initial
    // REST fetch populated the list.
    expect(globalStore.roomQueues.lobby.members.map((m) => m.user_id)).toEqual([1, 2])
    await vm.removeRoomMember({ user_id: 2, role: 'guest' })
    expect(apiMock.removeRoomMember).toHaveBeenCalledWith('lobby', 2)
    confirmSpy.mockRestore()
    wrapper.unmount()
    globalStore.clearUser()
  })

  // --- R10c 1008 close → removed state ---

  it('onClose with code 1008 marks the current viewer removed and renders the removed banner', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [{ user_id: 1, role: 'host' }] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'guest' })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    // 1008 = policy violation: the host removed this client.
    inst.onClose({ code: 1008, reason: 'removed from room', wasClean: false })
    await flushPromises()
    expect(globalStore.roomQueues.lobby.removed).toBe(true)
    const html = wrapper.html()
    expect(html).toContain('removed-banner')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('onClose with code 1008 is a no-op when the room is already archived (archived banner stays)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [{ user_id: 1, role: 'host' }] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'guest' })
    globalStore.setRoomQueueConnected('lobby', true)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.markRoomArchived('lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    inst.onClose({ code: 1008, reason: 'removed from room', wasClean: false })
    await flushPromises()
    // 1008 in an already-archived context is interpreted as part of
    // the global archive close — we MUST NOT also flip the
    // removed flag (would render both banners and confuse the user).
    expect(globalStore.roomQueues.lobby.removed).toBe(false)
    const html = wrapper.html()
    expect(html).toContain('archived-banner')
    expect(html).not.toContain('removed-banner')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('onClose with code 1000 (normal closure) does NOT flip removed', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [{ user_id: 1, role: 'host' }] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'guest' })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance
    inst.onClose({ code: 1000, reason: '', wasClean: true })
    await flushPromises()
    expect(globalStore.roomQueues.lobby.removed).toBe(false)
    wrapper.unmount()
    globalStore.clearUser()
  })
})
