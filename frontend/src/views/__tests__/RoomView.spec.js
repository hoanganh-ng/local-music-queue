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
  // R11a
  getRoomChatMessages: vi.fn(),
  sendRoomChatMessage: vi.fn(),
  // R05b2 player lease
  claimRoomPlayerLease: vi.fn(),
  heartbeatRoomPlayerLease: vi.fn(),
  releaseRoomPlayerLease: vi.fn(),
  getRoomPlayerLease: vi.fn(),
  // R05b2 archive poll
  getRoom: vi.fn(),
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

// Test routes room-view onto `/rooms/:slug` BEFORE mount so that
// `route.params.slug` is non-null inside onMounted. Without this,
// the component's setup-time `if (target == null) return` early-out
// skips the WS connect and subsequent `await router.push` from the
// test body lands AFTER the lifecycle hooks have run, leaving the
// test driving the previous test's wsClient via driveSync.
async function mountRoomViewAt(slug) {
  const { router, wrapper } = mountRoomView(slug)
  await router.push(`/rooms/${slug}`)
  // Push + drain microtasks so onMounted has finished its setup
  // and the WS client factory has been called for THIS slug.
  await flushPromises()
  return { router, wrapper }
}

describe('RoomView', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.roomQueues = {}
    vi.clearAllMocks()
    // R11a: default the chat API methods so the existing test
    // suite (which doesn't know about chat) keeps working. Tests
    // that exercise the chat panel override these mocks.
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    // R11a (corrective pass): the POST response is wrapped as
    // { message: { ... } } so the sender's local view can apply
    // the persisted row immediately. Tests that want a different
    // shape override this default.
    apiMock.sendRoomChatMessage.mockResolvedValue({ message: { id: 0, room_slug: '', sender: { user_id: 0, display_name: '' }, content: '', created_at: new Date(0).toISOString() } })
    // R05b2 player lease: default to no active lease (404) so existing
    // tests that don't know about the panel don't crash, and a sensible
    // holder response for tests that exercise lease state.
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('no lease'), { status: 404 }))
    apiMock.getRoom.mockResolvedValue({ id: 7, slug: 'lobby', status: 'active' })
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
    globalStore.setUser({ id: 1, display_name: 'Host', role: 'host' })
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
    globalStore.setUser({ id: 1, display_name: 'Host', role: 'host' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
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
    globalStore.setUser({ id: 1, display_name: 'Host', role: 'host' })
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
    globalStore.setUser({ id: 1, display_name: 'Host', role: 'host' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
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
    globalStore.setUser({ id: 1, display_name: 'Host', role: 'host' })
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

  // --- R05b1: Back button navigates to RoomEntry, not Dashboard ---

  it('handleBack navigates to RoomEntry (not Dashboard) per R05b1', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    // Capture router.push calls.
    const routerPush = vi.fn()
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/rooms/:slug', name: 'Room', component: RoomView, meta: { requiresAuth: true } },
        { path: '/rooms', name: 'RoomEntry', component: { template: '<div />' } },
        { path: '/', name: 'Dashboard', component: { template: '<div />' } },
      ],
    })
    const origPush = router.push
    router.push = (...args) => { routerPush(...args); return origPush.apply(router, args) }
    const wrapper = shallowMount(RoomView, { global: { plugins: [router] } })
    await router.push('/rooms/lobby')
    await flushPromises()
    await wrapper.find('.logout-btn').trigger('click')
    expect(routerPush).toHaveBeenCalledWith({ name: 'RoomEntry' })
    // Back must NOT navigate to Dashboard anymore.
    expect(routerPush).not.toHaveBeenCalledWith({ name: 'Dashboard' })
    wrapper.unmount()
  })
})

// --- R11a: room chat panel ---

describe('RoomView (R11a chat panel)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.roomQueues = {}
    vi.clearAllMocks()
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    // R05b2 default.
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('no lease'), { status: 404 }))
    apiMock.getRoom.mockResolvedValue({ id: 7, slug: 'lobby', status: 'active' })
    // R11a (corrective pass): the POST response is wrapped as
    // { message: { ... } } so the sender's local view can apply
    // the persisted row immediately. Tests that want a different
    // shape override this default.
    apiMock.sendRoomChatMessage.mockResolvedValue({ message: { id: 0, room_slug: '', sender: { user_id: 0, display_name: '' }, content: '', created_at: new Date(0).toISOString() } })
  })

  it('fetches chat history AFTER the first room_queue_sync (not on mount) and again on slug change', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [
      { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: 'first', created_at: '2026-01-01T00:00:00Z' },
    ] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // R11a (corrective pass): the GET MUST NOT fire before the WS
    // client confirms registration. The seed is triggered by the
    // first room_queue_sync event.
    expect(apiMock.getRoomChatMessages).not.toHaveBeenCalled()
    // The room entry may already be created by the queue sync
    // (room_queue_sync sets the per-room state), so the strict
    // check is on the chat-specific call count rather than on
    // the existence of the room entry.
    const inst = wsFactoryMock.lastInstance
    inst.onMessage({ type: 'room_queue_sync', data: { room_slug: 'lobby', state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] } } })
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledWith('lobby', 50)
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)

    // A second sync event MUST NOT trigger a re-seed (the flag
    // gates the seed to once per WS connection).
    inst.onMessage({ type: 'room_queue_sync', data: { room_slug: 'lobby', state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] } } })
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(1)

    // Navigate to a new slug; the chat history is re-seeded
    // after the next room_queue_sync event.
    apiMock.getRoomChatMessages.mockResolvedValueOnce({ messages: [
      { id: 2, room_slug: 'lounge', sender: { user_id: 2, display_name: 'B' }, content: 'second', created_at: '2026-01-01T00:00:01Z' },
    ] })
    await router.push('/rooms/lounge')
    await flushPromises()
    const inst2 = wsFactoryMock.lastInstance
    // The new slug's first sync triggers the seed.
    inst2.onMessage({ type: 'room_queue_sync', data: { room_slug: 'lounge', state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] } } })
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledWith('lounge', 50)
    expect(globalStore.roomQueues.lounge.messages[0].content).toBe('second')

    wrapper.unmount()
    globalStore.clearUser()
  })

  it('appends an incoming room_chat_message_created WS event to the local cache', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const inst = wsFactoryMock.lastInstance

    inst.onMessage({
      type: 'room_chat_message_created',
      data: {
        message: {
          id: 99, room_slug: 'lobby',
          sender: { user_id: 2, display_name: 'Peer' },
          content: 'hello peer', created_at: '2026-01-01T00:00:00Z',
        },
      },
    })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
    expect(globalStore.roomQueues.lobby.messages[0].content).toBe('hello peer')
    expect(globalStore.roomQueues.lobby.messages[0].sender.display_name).toBe('Peer')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('successful send calls api.sendRoomChatMessage and clears the input', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    apiMock.sendRoomChatMessage.mockResolvedValue({
      message: {
        id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' },
        content: 'hi', created_at: '2026-01-01T00:00:00Z',
      },
    })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // Fire the captured onOpen so the per-room WS connection is
    // considered connected (canChat requires connected=true).
    wsFactoryMock.lastInstance.onOpen()
    const vm = wrapper.vm
    vm.chatDraft = '  hi  '
    await vm.sendChat()
    await flushPromises()
    expect(apiMock.sendRoomChatMessage).toHaveBeenCalledWith('lobby', 'hi')
    expect(vm.chatDraft).toBe('')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('send button is disabled when disconnected, unauthenticated, archived, or removed', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const vm = wrapper.vm
    vm.chatDraft = 'hi'

    // Disconnected initially (no onOpen fired) — canChat must be false.
    expect(vm.canChat).toBe(false)

    // Mark connected.
    wsFactoryMock.lastInstance.onOpen()
    expect(vm.canChat).toBe(true)

    // Archived.
    globalStore.markRoomArchived('lobby')
    expect(vm.canChat).toBe(false)
    globalStore.roomQueues.lobby.archived = false

    // Removed.
    globalStore.markRoomRemovedAsCurrentUser('lobby')
    expect(vm.canChat).toBe(false)
    globalStore.roomQueues.lobby.removed = false

    // Unauthenticated.
    const prev = globalStore.currentUser
    globalStore.currentUser = null
    expect(vm.canChat).toBe(false)
    globalStore.currentUser = prev
    expect(vm.canChat).toBe(true)

    wrapper.unmount()
    globalStore.clearUser()
  })

  it('chat messages are rendered as text only — never as HTML (no v-html)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [
      { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: '<img src=x onerror=alert(1)>', created_at: 't' },
    ] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // R11a (corrective pass): the seed runs AFTER the first sync
    // event, so we have to drive the sync to populate the cache.
    wsFactoryMock.lastInstance.onMessage({ type: 'room_queue_sync', data: { room_slug: 'lobby', state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] } } })
    await flushPromises()
    const html = wrapper.html()
    // The dangerous string is rendered as the literal text content
    // (escaped angle brackets), not as an <img> element. We assert
    // the string appears and that no actual <img> tag is present.
    expect(html).toContain('&lt;img')
    expect(html).not.toMatch(/<img\b[^>]*src=x/)
    wrapper.unmount()
    globalStore.clearUser()
  })
})

// --- R11a corrective-pass tests ---

describe('RoomView (R11a corrective pass)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.roomQueues = {}
    vi.clearAllMocks()
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    apiMock.sendRoomChatMessage.mockResolvedValue({ message: { id: 0, room_slug: '', sender: { user_id: 0, display_name: '' }, content: '', created_at: new Date(0).toISOString() } })
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('no lease'), { status: 404 }))
    apiMock.getRoom.mockResolvedValue({ id: 7, slug: 'lobby', status: 'active' })
  })

  function driveSync(slug = 'lobby', state = { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] }) {
    wsFactoryMock.lastInstance.onMessage({ type: 'room_queue_sync', data: { room_slug: slug, state } })
  }

  it('a WS chat event arriving while the history GET is in flight does not duplicate', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    // The GET is intentionally slow so we can inject a WS event
    // while it's pending. The store merge must collapse the
    // post-resolution WS entry + the GET result on the same id.
    let resolveGet
    apiMock.getRoomChatMessages.mockImplementation(() => new Promise((r) => { resolveGet = r }))

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync()
    await flushPromises() // GET is in flight

    // WS chat event for id=42 arrives BEFORE the GET resolves.
    wsFactoryMock.lastInstance.onMessage({
      type: 'room_chat_message_created',
      data: {
        message: { id: 42, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' }, content: 'first', created_at: '2026-01-01T00:00:00Z' },
      },
    })

    // The GET returns with id=42 included (the server already
    // saw the message before sending the page). The store merge
    // must NOT produce a duplicate row.
    resolveGet({ messages: [
      { id: 42, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' }, content: 'first', created_at: '2026-01-01T00:00:00Z' },
    ] })
    await flushPromises()
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
    expect(globalStore.roomQueues.lobby.messages[0].id).toBe(42)
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('a message sent before WS registration appears in the post-sync history fetch', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    // The peer sent id=99 between mount and sync registration.
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [
      { id: 99, room_slug: 'lobby', sender: { user_id: 2, display_name: 'Peer' }, content: 'before-you-arrived', created_at: '2026-01-01T00:00:00Z' },
    ] })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    // Before the sync, the seed has NOT run, so the chat cache
    // is empty (no pre-WS GET race).
    expect(globalStore.roomQueues.lobby == null || globalStore.roomQueues.lobby.messages.length === 0).toBe(true)
    driveSync()
    await flushPromises()
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
    expect(globalStore.roomQueues.lobby.messages[0].id).toBe(99)
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('POST response is applied immediately, then a duplicate WS event stays as one row', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    apiMock.sendRoomChatMessage.mockResolvedValue({
      message: {
        id: 7, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' },
        content: 'hi', created_at: '2026-01-01T00:00:00Z',
      },
    })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    wsFactoryMock.lastInstance.onOpen()
    driveSync()
    await flushPromises()

    const vm = wrapper.vm
    vm.chatDraft = 'hi'
    await vm.sendChat()
    await flushPromises()
    // POST response is applied immediately.
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
    expect(globalStore.roomQueues.lobby.messages[0].id).toBe(7)
    // The hub later delivers the room_chat_message_created event
    // for the same id. The store merge must dedupe.
    wsFactoryMock.lastInstance.onMessage({
      type: 'room_chat_message_created',
      data: {
        message: { id: 7, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' }, content: 'hi', created_at: '2026-01-01T00:00:00Z' },
      },
    })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('onGap fetches and merges chat history into the existing cache (no destructive clear)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync()
    await flushPromises()

    // Inject a known row via the WS event so the cache has one
    // message before the gap-driven re-fetch.
    wsFactoryMock.lastInstance.onMessage({
      type: 'room_chat_message_created',
      data: {
        message: { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' }, content: 'local', created_at: '2026-01-01T00:00:00Z' },
      },
    })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)

    // The onGap fetch returns a SECOND message; the merge must
    // append, not replace, so the local row stays put.
    apiMock.getRoomChatMessages.mockResolvedValueOnce({ messages: [
      { id: 2, room_slug: 'lobby', sender: { user_id: 2, display_name: 'Peer' }, content: 'peer', created_at: '2026-01-01T00:00:01Z' },
    ] })
    await wsFactoryMock.lastInstance.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })
    await flushPromises()
    const ids = globalStore.roomQueues.lobby.messages.map((m) => m.id)
    expect(ids).toEqual([1, 2])
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('onGap chat fetch failure does NOT clear the cached messages', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync()
    await flushPromises()

    wsFactoryMock.lastInstance.onMessage({
      type: 'room_chat_message_created',
      data: {
        message: { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' }, content: 'local', created_at: '2026-01-01T00:00:00Z' },
      },
    })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)

    // The onGap queue recovery succeeds, but the chat recovery
    // fails. The local message must remain.
    apiMock.getRoomChatMessages.mockRejectedValueOnce(Object.assign(new Error('boom'), { status: 500 }))
    await wsFactoryMock.lastInstance.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })
    await flushPromises()
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
    expect(globalStore.roomQueues.lobby.messages[0].id).toBe(1)
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('a stale history GET (slug changed mid-flight) does not pollute the new room', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    // First room's GET is intentionally slow.
    let resolveLobbyGet
    apiMock.getRoomChatMessages.mockImplementationOnce(() => new Promise((r) => { resolveLobbyGet = r }))

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync('lobby')
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(1)

    // Navigate to a new slug before the first GET resolves.
    await router.push('/rooms/lounge')
    await flushPromises()
    // The new room drives a new sync + a new GET (slug change
    // resets chatHistoryFetched).
    driveSync('lounge')
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(2)

    // Now resolve the FIRST GET with lobby data. The store
    // mutator must NOT apply it to the current room (lounge).
    resolveLobbyGet({ messages: [
      { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' }, content: 'lobby-stale', created_at: '2026-01-01T00:00:00Z' },
    ] })
    await flushPromises()
    // Lounge remains empty (its own GET has its own mock that
    // resolves with no messages).
    expect(globalStore.roomQueues.lounge == null || globalStore.roomQueues.lounge.messages.length === 0).toBe(true)
    // The lobby entry may still be created by the queue sync
    // and other seeds, but its messages slice must be empty
    // because the GET resolution is stale.
    if (globalStore.roomQueues.lobby) {
      expect(globalStore.roomQueues.lobby.messages.length).toBe(0)
    }
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('chatDraft over the 500-code-point cap disables the Send button', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    wsFactoryMock.lastInstance.onOpen()
    driveSync()
    await flushPromises()

    const vm = wrapper.vm
    // 501 code points (the cap is 500) — Send must be disabled.
    vm.chatDraft = Array.from({ length: 501 }).map(() => 'a').join('')
    expect(vm.chatDraftCodePoints).toBe(501)
    expect(vm.chatDraftOverLimit).toBe(true)
    expect(vm.canChat).toBe(false)
    // 500 code points — Send must be enabled.
    vm.chatDraft = Array.from({ length: 500 }).map(() => 'a').join('')
    expect(vm.chatDraftOverLimit).toBe(false)
    expect(vm.canChat).toBe(true)
    // 1 emoji (1 code point) — Send must be enabled.
    vm.chatDraft = '🌍'
    expect(vm.chatDraftCodePoints).toBe(1)
    expect(vm.chatDraftOverLimit).toBe(false)
    wrapper.unmount()
    globalStore.clearUser()
  })
})

// --- R11a final corrective pass tests ---

describe('RoomView (R11a final corrective pass)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.roomQueues = {}
    vi.clearAllMocks()
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    apiMock.sendRoomChatMessage.mockResolvedValue({ message: { id: 0, room_slug: '', sender: { user_id: 0, display_name: '' }, content: '', created_at: new Date(0).toISOString() } })
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('no lease'), { status: 404 }))
    apiMock.getRoom.mockResolvedValue({ id: 7, slug: 'lobby', status: 'active' })
  })

  function driveSync(slug = 'lobby', state = { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] }) {
    wsFactoryMock.lastInstance.onMessage({ type: 'room_queue_sync', data: { room_slug: slug, state } })
  }

  // --- Item 1: retry the initial history seed after failure ---

  it('retries the chat history seed on a later room_queue_sync after an initial GET failure', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    // First sync triggers a GET that fails; the second sync is
    // allowed to retry it.
    apiMock.getRoomChatMessages
      .mockRejectedValueOnce(Object.assign(new Error('boom'), { status: 500 }))
      .mockResolvedValueOnce({ messages: [
        { id: 11, room_slug: 'lobby', sender: { user_id: 2, display_name: 'Peer' }, content: 'first', created_at: '2026-01-01T00:00:00Z' },
      ] })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync()
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(1)
    // The previous messages slice is preserved on failure.
    if (globalStore.roomQueues.lobby) {
      expect(globalStore.roomQueues.lobby.messages || []).toEqual([])
    }

    // A second room_queue_sync must be allowed to retry (because
    // chatHistoryFetched stayed false after the failure).
    driveSync()
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(2)
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
    expect(globalStore.roomQueues.lobby.messages[0].id).toBe(11)

    wrapper.unmount()
    globalStore.clearUser()
  })

  it('a successful retry merges seeded messages into the existing cache', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ messages: [] })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    // First sync: GET returns successfully with id=1.
    apiMock.getRoomChatMessages.mockResolvedValueOnce({ messages: [
      { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: 'a', created_at: '2026-01-01T00:00:00Z' },
    ] })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync()
    await flushPromises()
    expect(globalStore.roomQueues.lobby.messages.map((m) => m.id)).toEqual([1])

    // After a successful seed, chatHistoryFetched is true, so a
    // second sync must NOT re-fetch.
    driveSync()
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(1)
    expect(globalStore.roomQueues.lobby.messages.map((m) => m.id)).toEqual([1])

    wrapper.unmount()
    globalStore.clearUser()
  })

  it('two syncs while the first GET is in flight produce exactly one network request', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    let resolveGet
    apiMock.getRoomChatMessages.mockImplementation(() => new Promise((r) => { resolveGet = r }))

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync() // first sync → GET starts
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(1)

    // Second sync arrives while first GET is pending → in-flight
    // guard suppresses a second request.
    driveSync()
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(1)

    // Third sync while GET is still pending → still no second request.
    driveSync()
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(1)

    // Resolve the GET → a further sync must NOT re-fetch
    // (chatHistoryFetched is true after the merge).
    resolveGet({ messages: [
      { id: 5, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: 'a', created_at: '2026-01-01T00:00:00Z' },
    ] })
    await flushPromises()
    driveSync()
    await flushPromises()
    expect(apiMock.getRoomChatMessages).toHaveBeenCalledTimes(1)

    wrapper.unmount()
    globalStore.clearUser()
  })

  // --- Item 2: independent recovery on gap ---

  it('onGap recovers chat history even when queue recovery rejects', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync()
    await flushPromises()

    // Queue recovery rejects, chat recovery resolves.
    apiMock.getRoomQueue.mockRejectedValueOnce(Object.assign(new Error('forbidden'), { status: 403 }))
    apiMock.getRoomChatMessages.mockResolvedValueOnce({ messages: [
      { id: 42, room_slug: 'lobby', sender: { user_id: 2, display_name: 'Peer' }, content: 'peer', created_at: '2026-01-01T00:00:00Z' },
    ] })

    await wsFactoryMock.lastInstance.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })
    await flushPromises()

    // The chat recovery succeeded, so the message is merged into
    // the cache even though the queue recovery failed.
    expect(globalStore.roomQueues.lobby.messages.map((m) => m.id)).toEqual([42])
    // The queue error is surfaced via toast (existing behavior).
    expect(toastMock.error).toHaveBeenCalled()

    wrapper.unmount()
    globalStore.clearUser()
  })

  it('onGap keeps existing chat rows when chat recovery rejects (queue recovery succeeds)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [{ id: 'restored' }], current_index: 0, current_song: { id: 'restored' }, status: 'paused', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    driveSync()
    await flushPromises()

    // Seed a known local chat row via the WS event.
    wsFactoryMock.lastInstance.onMessage({
      type: 'room_chat_message_created',
      data: {
        message: { id: 7, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' }, content: 'local', created_at: '2026-01-01T00:00:00Z' },
      },
    })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)

    // Chat recovery rejects; queue recovery succeeds.
    apiMock.getRoomChatMessages.mockRejectedValueOnce(Object.assign(new Error('boom'), { status: 500 }))
    await wsFactoryMock.lastInstance.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })
    await flushPromises()

    // Queue recovered.
    expect(globalStore.roomQueues.lobby.state.songs[0].id).toBe('restored')
    // Local chat row is preserved (no destructive clear).
    expect(globalStore.roomQueues.lobby.messages.map((m) => m.id)).toEqual([7])
    // The queue recovery did NOT produce a toast (it succeeded).
    expect(toastMock.error).not.toHaveBeenCalled()

    wrapper.unmount()
    globalStore.clearUser()
  })

  it('stale onGap finalization does not recreate a cleared old-room entry (full lifecycle)', async () => {
    // R11a (final lifecycle correction): exercise the actual
    // onGap path end-to-end, not just the IfExists mutators. The
    // old room's WebSocket client fires onGap; the recovery GETs
    // settle while the user navigates to a new room (which clears
    // the old room entry and bumps the seqGapGeneration token).
    // The finally block must NOT recreate the old room entry,
    // must NOT mutate the new room's state, and must NOT raise a
    // stale toast.
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })

    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    // Mock ordering mirrors the working "onGap keeps existing
    // chat rows" test pattern. Mock queue per call (vi consumes
    // onceImpls in FIFO order):
    //   1. lobby sync GET  → empty list (chat)
    //   2. onGap queue GET → 500 reject (queue)
    //   3. onGap chat GET  → 500 reject (chat)
    //   4. lounge sync GET → new-room id 200 (chat)
    // The default fall-back (mockResolvedValue({messages:[]}))
    // covers any extra calls.
    apiMock.getRoomChatMessages.mockResolvedValueOnce({ messages: [] })
    apiMock.getRoomQueue.mockRejectedValueOnce(Object.assign(new Error('queue boom'), { status: 500 }))
    apiMock.getRoomChatMessages.mockRejectedValueOnce(Object.assign(new Error('chat boom'), { status: 500 }))
    apiMock.getRoomChatMessages.mockResolvedValueOnce({ messages: [
      { id: 200, room_slug: 'lounge', sender: { user_id: 9, display_name: 'New' }, content: 'lounge-fresh', created_at: '2026-01-01T00:00:00Z' },
    ] })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    // The slug watcher's body is async (`await seedStateFromRest`
    // etc.); drain extra microtasks so the lobby's WS client is
    // built before driveSync fires.
    await flushPromises()
    await flushPromises()
    driveSync()
    await flushPromises()

    // Capture the WS client the current mount wired (i.e. the
    // one that owns this slug's onGap). wsFactoryMock.lastInstance
    // may be a client from a previous test in the suite; using
    // the captured reference guarantees we're driving THIS
    // mount's onGap.
    const lobbyClient = wsFactoryMock.lastInstance

    // Pre-condition: the lobby entry exists and its recovery
    // flag is wired by the new IfExists init call.
    expect(globalStore.roomQueues.lobby).toBeTruthy()

    // Fire onGap. Both recovery GETs reject synchronously; the
    // queue IIFE's catch path would normally toast, but the
    // targetSlug !== slug.value guard at the top of the catch
    // will short-circuit it once the user navigates away.
    const gapPromise = lobbyClient.onGap({ slug: 'lobby', lastSeqNum: 1, seqNum: 3 })

    // Navigate to the new slug BEFORE awaiting the onGap
    // promise. The slug watcher runs teardownCurrentClient,
    // which:
    //   - disconnects the lobby ws client
    //   - clears the lobby entry from the store
    //   - bumps the seqGapGeneration token
    //   - resets suppressSeqGap
    await router.push('/rooms/lounge')
    await flushPromises()
    driveSync('lounge')
    await flushPromises()

    // Confirm the old room's entry was cleared by teardown and
    // the new room established its own entry.
    expect(globalStore.roomQueues.lobby).toBeUndefined()
    expect(globalStore.roomQueues.lounge).toBeTruthy()
    expect(globalStore.roomQueues.lounge.messages.map((m) => m.id)).toEqual([200])

    // Await the original lobby onGap promise. Both IIFEs have
    // already settled (their GETs were mocked to reject), and
    // the finally block runs.
    await gapPromise
    await flushPromises()

    // Old-room invariants:
    //   - the lobby entry MUST remain absent. Neither the IIFEs'
    //     IfExists mutators NOR the finally block's IfExists
    //     recoveryInFlight may have recreated it.
    expect(globalStore.roomQueues.lobby).toBeUndefined()

    // New-room invariants:
    //   - the lounge entry remains present.
    //   - its messages slice is unchanged.
    //   - its queue state is unchanged.
    //   - its recoveryInFlight is owned by its own onGap path;
    //     the stale lobby finally MUST NOT have touched it.
    expect(globalStore.roomQueues.lounge).toBeTruthy()
    expect(globalStore.roomQueues.lounge.messages.map((m) => m.id)).toEqual([200])
    expect(globalStore.roomQueues.lounge.state.songs || []).toEqual([])
    expect(globalStore.roomQueues.lounge.recoveryInFlight).not.toBe(true)
    // No stale lobby error visible on the new room.
    expect(globalStore.roomQueues.lounge.lastError).toBeNull()
    expect(globalStore.roomQueues.lounge.removed || false).toBe(false)
    expect(globalStore.roomQueues.lounge.archived || false).toBe(false)

    // No stale toast was raised. The two error branches inside
    // the IIFEs are gated on the same stale-guard, so neither
    // a "could not resync" toast nor a 401/403/404/409 toast
    // should have been emitted. The success path never toasts.
    expect(toastMock.error).not.toHaveBeenCalled()
    expect(toastMock.info).not.toHaveBeenCalled()
    expect(toastMock.success).not.toHaveBeenCalled()

    wrapper.unmount()
    globalStore.clearUser()
  })

  // --- Item 3: 500 code-point input behavior ---

  it('accepts exactly 500 emoji / code points (input NOT capped at 500 UTF-16 code units)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    wsFactoryMock.lastInstance.onOpen()
    driveSync()
    await flushPromises()

    const vm = wrapper.vm
    vm.chatDraft = '🌍'.repeat(500)
    expect(vm.chatDraftCodePoints).toBe(500)
    expect(vm.chatDraftOverLimit).toBe(false)
    // Send button must NOT be disabled at exactly 500.
    expect(vm.canChat).toBe(true)
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('shows the over-limit hint and disables Send at 501 code points', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    wsFactoryMock.lastInstance.onOpen()
    driveSync()
    await flushPromises()

    const vm = wrapper.vm
    vm.chatDraft = '🌍'.repeat(501)
    await flushPromises()
    expect(vm.chatDraftCodePoints).toBe(501)
    expect(vm.chatDraftOverLimit).toBe(true)
    expect(vm.canSendChat).toBe(false)
    // The over-limit hint is visible in the rendered DOM and the
    // Send button is rendered disabled.
    const html = wrapper.html()
    expect(html).toContain('chat-over-limit')
    const sendBtn = wrapper.find('[data-testid="chat-send"]')
    expect(sendBtn.attributes('disabled')).toBeDefined()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('keeps the input enabled and editable at 501 code points (user must be able to shorten)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    wsFactoryMock.lastInstance.onOpen()
    driveSync()
    await flushPromises()

    const vm = wrapper.vm
    vm.chatDraft = 'x'.repeat(501)
    expect(vm.chatDraftOverLimit).toBe(true)
    // canEditChat covers interactivity (auth + WS + active) and
    // intentionally does NOT depend on chatDraftOverLimit so the
    // input stays enabled when the draft exceeds the cap.
    expect(vm.canEditChat).toBe(true)
    // canSendChat does gate on the cap, so Send is disabled.
    expect(vm.canSendChat).toBe(false)

    // The input has no native maxlength attribute (R11a final
    // corrective pass): the user is free to type past 500 and the
    // input keeps the typed text.
    const input = wrapper.find('[data-testid="chat-input"]')
    expect(input.attributes('maxlength')).toBeUndefined()
    // And the bound draft is preserved (the input is not capped
    // nor auto-trimmed by Vue when over the limit).
    expect(vm.chatDraft.length).toBe(501)

    wrapper.unmount()
    globalStore.clearUser()
  })

  it('shortening from 501 to 500 re-enables Send', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    apiMock.getRoomAutoQueueStatus.mockResolvedValue({ enabled: false, strategy: 'related' })
    apiMock.getRoomMembers.mockResolvedValue({ members: [] })
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })

    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    wsFactoryMock.lastInstance.onOpen()
    driveSync()
    await flushPromises()

    const vm = wrapper.vm
    vm.chatDraft = 'a'.repeat(501)
    expect(vm.chatDraftOverLimit).toBe(true)
    expect(vm.canSendChat).toBe(false)

    // Trim the trailing character — the draft now sits at exactly 500.
    vm.chatDraft = 'a'.repeat(500)
    expect(vm.chatDraftOverLimit).toBe(false)
    // Non-empty + within cap + canEditChat → canSendChat is true.
    expect(vm.canSendChat).toBe(true)

    wrapper.unmount()
    globalStore.clearUser()
  })
})

// --- R05b2: player-lease UI ---

describe('RoomView (R05b2 player-lease UI)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.roomQueues = {}
    vi.resetAllMocks()
    apiMock.getRoomChatMessages.mockResolvedValue({ messages: [] })
    apiMock.sendRoomChatMessage.mockResolvedValue({ message: { id: 0, room_slug: '', sender: { user_id: 0, display_name: '' }, content: '', created_at: new Date(0).toISOString() } })
    apiMock.claimRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.heartbeatRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.releaseRoomPlayerLease.mockResolvedValue(null)
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('no lease'), { status: 404 }))
    apiMock.getRoom.mockResolvedValue({ id: 7, slug: 'lobby', status: 'active' })
  })

  it('renders an always-visible Player device panel even with an empty queue', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    const html = wrapper.html()
    expect(html).toContain('player-device-panel')
    // With no active lease (404 default) the panel renders 'No active player.'
    expect(html).toContain('No active player')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('derives room-host status from the membership list (not currentUser.role)', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    // Globally a host but only a room guest → must NOT be host here.
    globalStore.setUser({ id: 99, display_name: 'Peer', role: 'host' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 99, role: 'guest' }] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    expect(vm.isHost).toBe(false)
    expect(wrapper.html()).not.toContain('claim-player-btn')
    expect(wrapper.html()).not.toContain('host-panel')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('a globally ordinary user who is the room host can claim', async () => {
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'guest' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    expect(vm.isHost).toBe(true)
    expect(wrapper.html()).toContain('claim-player-btn')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('all active members can see the Player device panel state', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 7, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 2, display_name: 'Guest', role: 'guest' })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const html = wrapper.html()
    expect(html).toContain('player-device-panel')
    expect(html).toContain('user #7')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('only the current holder may use direct playback controls', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.getRoomQueue.mockResolvedValue({ songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'paused', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    // Holder: playback controls enabled. (Pause is disabled because
    // status is already 'paused' — that gating is independent of the
    // lease holder. We assert play / skip / volume enablement for the
    // holder case.)
    expect(wrapper.find('[data-testid="play-btn"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="skip-btn"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-testid="vol-up-btn"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
    globalStore.clearUser()

    // Non-holder: queue another holder response for the SECOND mount,
    // then mount RoomView as a DIFFERENT user who is NOT the holder.
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    globalStore.setUser({ id: 99, display_name: 'Peer', role: 'guest' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }, { user_id: 99, role: 'guest' }] })
    const { wrapper: w3, router: r3 } = mountRoomView()
    await r3.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    expect(w3.find('[data-testid="play-btn"]').attributes('disabled')).toBeDefined()
    expect(w3.find('[data-testid="skip-btn"]').attributes('disabled')).toBeDefined()
    expect(w3.find('[data-testid="vol-up-btn"]').attributes('disabled')).toBeDefined()
    w3.unmount()
    globalStore.clearUser()
  })

  it('renders the "Release player and archive room" button when host + holder', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'guest' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    expect(wrapper.html()).toContain('Release player and archive room')
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('release confirmation copy explicitly mentions archival and is not a transfer', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'guest' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    await vm.releasePlayer()
    expect(confirmSpy).toHaveBeenCalled()
    const message = confirmSpy.mock.calls[0][0]
    expect(message).toMatch(/archive/i)
    expect(message).toMatch(/not a transfer/i)
    confirmSpy.mockRestore()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('does NOT call release on Back / unmount navigation', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.getRoomQueue.mockResolvedValue({ songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    apiMock.releaseRoomPlayerLease.mockClear()
    await router.push('/rooms')
    await flushPromises()
    wrapper.unmount()
    expect(apiMock.releaseRoomPlayerLease).not.toHaveBeenCalled()
    wrapper.unmount()
    globalStore.clearUser()
  })

  it('expired-lease 410 toast copy does NOT suggest reclaiming', async () => {
    apiMock.setRoomPlaybackStatus.mockRejectedValueOnce(Object.assign(new Error('gone'), { status: 410 }))
    apiMock.getRoomQueue.mockResolvedValue({ songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'paused', queue: [], history: [] })
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
    const { wrapper, router } = mountRoomView()
    await router.push('/rooms/lobby')
    await flushPromises()
    globalStore.setRoomQueueConnected('lobby', true)
    await flushPromises()
    const vm = wrapper.vm
    await vm.setPlaybackStatus('playing')
    expect(toastMock.error).toHaveBeenCalled()
    const copy = toastMock.error.mock.calls[toastMock.error.mock.calls.length - 1][0]
    expect(copy).not.toMatch(/reclaim/i)
    expect(copy).toMatch(/archiving|archived|expired/i)
    wrapper.unmount()
    globalStore.clearUser()
  })
})
