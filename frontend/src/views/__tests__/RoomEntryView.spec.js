import { describe, it, expect, beforeEach, vi } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { globalStore } from '../../store'
import { sessionHelper } from '../../services/session'

const apiMock = vi.hoisted(() => ({
  listRooms: vi.fn(),
  getRoom: vi.fn(),
  createRoom: vi.fn(),
  redeemInvite: vi.fn(),
}))
vi.mock('../../services/api', () => ({ api: apiMock }))

const toastMock = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn(), info: vi.fn() }))
vi.mock('../../composables/useToast', () => ({ useToast: () => toastMock }))

const pushMock = vi.hoisted(() => vi.fn())
vi.mock('vue-router', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useRouter: () => ({ push: pushMock }),
  }
})

import RoomEntryView from '../RoomEntryView.vue'

function mountRoomEntry() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/rooms', name: 'RoomEntry', component: RoomEntryView, meta: { requiresAuth: true } },
      { path: '/rooms/:slug', name: 'Room', component: { template: '<div />' } },
      { path: '/', name: 'Dashboard', component: { template: '<div />' } },
      { path: '/auth', name: 'Auth', component: { template: '<div />' } },
    ],
  })
  const wrapper = shallowMount(RoomEntryView, { global: { plugins: [router] } })
  return { router, wrapper }
}

describe('RoomEntryView', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.clearUser()
    // resetAllMocks clears implementations AND queued once-resolves so
    // a stale `mockResolvedValueOnce([])` from a previous test does
    // not leak into the next one. clearAllMocks only resets call
    // history, which is not enough.
    vi.resetAllMocks()
    apiMock.listRooms.mockResolvedValue([])
    apiMock.getRoom.mockResolvedValue({ id: 1, slug: 'lobby', name: 'Lobby', status: 'active' })
    apiMock.createRoom.mockResolvedValue({ id: 2, slug: 'newroom', name: 'New', status: 'active' })
    apiMock.redeemInvite.mockResolvedValue({ room_id: 1, user_id: 1, role: 'guest', joined_at: 't' })
  })

  it('fetches active rooms on mount and renders the list', async () => {
    apiMock.listRooms.mockResolvedValueOnce([
      { id: 1, slug: 'lobby', name: 'Lobby', status: 'active' },
      { id: 2, slug: 'lounge', name: 'Lounge', status: 'active' },
    ])
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    expect(apiMock.listRooms).toHaveBeenCalledWith('active')
    const html = wrapper.html()
    expect(html).toContain('Active rooms')
    expect(html).toContain('Lobby')
    expect(html).toContain('lobby')
    expect(html).toContain('Lounge')
  })

  it('renders the empty hint when the active-room list is empty', async () => {
    apiMock.listRooms.mockResolvedValueOnce([])
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    expect(wrapper.find('[data-testid="room-list-empty"]').exists()).toBe(true)
  })

  it('renders the loading state during the in-flight refresh', async () => {
    let resolveList
    apiMock.listRooms.mockImplementation(() => new Promise((r) => { resolveList = r }))
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    expect(wrapper.find('[data-testid="room-list-loading"]').exists()).toBe(true)
    resolveList([])
    await flushPromises()
    expect(wrapper.find('[data-testid="room-list-loading"]').exists()).toBe(false)
    // Restore the default mock for the next test.
    apiMock.listRooms.mockResolvedValue([])
  })

  it('surfaces a clear 401 error when the active-room list fetch is unauthorized', async () => {
    apiMock.listRooms.mockRejectedValueOnce(Object.assign(new Error('unauthorized'), { status: 401 }))
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const errEl = wrapper.find('[data-testid="list-error"]')
    expect(errEl.exists()).toBe(true)
    expect(errEl.text()).toMatch(/signed out/i)
  })

  it('a stale refresh response does NOT overwrite a newer refresh result', async () => {
    const older = [
      { id: 1, slug: 'old', name: 'Old', status: 'active' },
    ]
    const newer = [
      { id: 1, slug: 'old', name: 'Old', status: 'active' },
      { id: 2, slug: 'new', name: 'New', status: 'active' },
    ]
    let resolveOlder
    apiMock.listRooms
      .mockImplementationOnce(() => new Promise((r) => { resolveOlder = r }))
      .mockResolvedValueOnce(newer)
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    // Mount started the first refresh; trigger a manual one.
    const refreshBtn = wrapper.find('[data-testid="refresh-rooms-btn"]')
    await refreshBtn.trigger('click')
    await flushPromises()
    // The newer refresh has already resolved. Now resolve the older
    // one — its result MUST NOT overwrite the newer list.
    resolveOlder(older)
    await flushPromises()
    const html = wrapper.html()
    expect(html).toContain('new')
    // Restore the default mock for the next test.
    apiMock.listRooms.mockResolvedValue([])
  })

  it('opens a listed room on click without making an additional membership probe', async () => {
    apiMock.listRooms.mockResolvedValueOnce([
      { id: 1, slug: 'lobby', name: 'Lobby', status: 'active' },
    ])
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    apiMock.listRooms.mockClear()
    // openListedRoom is the click handler behind the inline Open
    // button. Verify that calling it directly navigates without
    // probing membership.
    wrapper.vm.openListedRoom({ id: 1, slug: 'lobby', name: 'Lobby', status: 'active' })
    expect(pushMock).toHaveBeenCalledWith({ name: 'Room', params: { slug: 'lobby' } })
    // No additional GET /rooms/{slug} call — the listed room is
    // navigated to directly per the R05b1 contract.
    expect(apiMock.getRoom).not.toHaveBeenCalled()
  })

  it('renders an Open button for each listed room with the per-room data-testid', async () => {
    apiMock.listRooms.mockResolvedValueOnce([
      { id: 1, slug: 'lobby', name: 'Lobby', status: 'active' },
      { id: 2, slug: 'lounge', name: 'Lounge', status: 'active' },
    ])
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const html = wrapper.html()
    expect(html).toContain('open-room-lobby')
    expect(html).toContain('open-room-lounge')
  })

  it('manual open by slug: active room navigates; 400/401/404 surface clear errors', async () => {
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm

    // Active room → navigates
    apiMock.getRoom.mockResolvedValueOnce({ id: 1, slug: 'lobby', name: 'Lobby', status: 'active' })
    vm.manualSlug = 'lobby'
    await vm.openBySlug()
    expect(apiMock.getRoom).toHaveBeenCalledWith('lobby')
    expect(pushMock).toHaveBeenCalledWith({ name: 'Room', params: { slug: 'lobby' } })

    // 400 invalid slug
    pushMock.mockClear()
    apiMock.getRoom.mockRejectedValueOnce(Object.assign(new Error('invalid'), { status: 400 }))
    vm.manualSlug = 'BAD!!'
    await vm.openBySlug()
    expect(wrapper.find('[data-testid="manual-open-error"]').text()).toMatch(/invalid/i)

    // 404 not found
    vm.manualSlug = 'missing'
    apiMock.getRoom.mockRejectedValueOnce(Object.assign(new Error('not found'), { status: 404 }))
    await vm.openBySlug()
    expect(wrapper.find('[data-testid="manual-open-error"]').text()).toMatch(/not found/i)

    // 401 signed out
    vm.manualSlug = 'lobby'
    apiMock.getRoom.mockRejectedValueOnce(Object.assign(new Error('unauthorized'), { status: 401 }))
    await vm.openBySlug()
    expect(wrapper.find('[data-testid="manual-open-error"]').text()).toMatch(/signed out/i)

    // Archived room: surface the unavailable message AND MUST NOT
    // navigate. The user remains on RoomEntry; archived rooms
    // cannot be opened or joined.
    pushMock.mockClear()
    vm.manualSlug = 'archive'
    apiMock.getRoom.mockResolvedValueOnce({ id: 1, slug: 'archive', name: 'Archive', status: 'archived' })
    await vm.openBySlug()
    expect(wrapper.find('[data-testid="manual-open-error"]').text()).toMatch(/no longer available/i)
    expect(pushMock).not.toHaveBeenCalled()
  })

  it('create room success clears the form, refreshes the list, and navigates', async () => {
    apiMock.listRooms.mockResolvedValueOnce([])
    apiMock.createRoom.mockResolvedValueOnce({ id: 2, slug: 'newroom', name: 'New Room', status: 'active' })
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    vm.createSlug = 'newroom'
    vm.createName = 'New Room'
    apiMock.listRooms.mockClear()
    await vm.submitCreateRoom()
    expect(apiMock.createRoom).toHaveBeenCalledWith('newroom', 'New Room')
    // Form cleared
    expect(vm.createSlug).toBe('')
    expect(vm.createName).toBe('')
    // List refreshed
    expect(apiMock.listRooms).toHaveBeenCalledWith('active')
    // Navigated
    expect(pushMock).toHaveBeenCalledWith({ name: 'Room', params: { slug: 'newroom' } })
  })

  it('create room maps 400 / 401 / 409 / 500 to clear error messages', async () => {
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    for (const status of [400, 401, 409, 500]) {
      apiMock.createRoom.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status }))
      vm.createSlug = 'lobby'
      vm.createName = 'Lobby'
      await vm.submitCreateRoom()
      const errEl = wrapper.find('[data-testid="create-error"]')
      expect(errEl.exists()).toBe(true)
    }
  })

  it('create room trims whitespace before submitting slug and name', async () => {
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    vm.createSlug = '  newroom  '
    vm.createName = '  New Room  '
    await vm.submitCreateRoom()
    expect(apiMock.createRoom).toHaveBeenCalledWith('newroom', 'New Room')
  })

  it('create room does NOT silently rewrite slug beyond trimming', async () => {
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    // Backend is authoritative; the view must NOT normalize the
    // slug (e.g. lower-case, collapse dashes) before sending.
    vm.createSlug = 'Mixed-Case'
    vm.createName = 'Mixed Case'
    await vm.submitCreateRoom()
    expect(apiMock.createRoom).toHaveBeenCalledWith('Mixed-Case', 'Mixed Case')
  })

  it('invite redemption resolves room_id to slug via a refreshed active-room list and navigates', async () => {
    apiMock.listRooms
      .mockResolvedValueOnce([]) // mount refresh
      .mockResolvedValueOnce([
        { id: 1, slug: 'old', name: 'Old', status: 'active' },
        { id: 7, slug: 'target', name: 'Target', status: 'active' },
      ])
    apiMock.redeemInvite.mockResolvedValueOnce({ room_id: 7, user_id: 1, role: 'guest', joined_at: 't' })
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    vm.inviteToken = 'sensitive-token-abc'
    await vm.submitRedeemInvite()
    expect(apiMock.redeemInvite).toHaveBeenCalledWith('sensitive-token-abc')
    expect(apiMock.listRooms).toHaveBeenCalledTimes(2)
    expect(pushMock).toHaveBeenCalledWith({ name: 'Room', params: { slug: 'target' } })
    // Token cleared after success.
    expect(vm.inviteToken).toBe('')
  })

  it('invite token is cleared after a successful redemption', async () => {
    apiMock.redeemInvite.mockResolvedValueOnce({ room_id: 1, user_id: 1, role: 'guest', joined_at: 't' })
    apiMock.listRooms.mockResolvedValue([{ id: 1, slug: 'lobby', name: 'Lobby', status: 'active' }])
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    vm.inviteToken = 'secret-token'
    await vm.submitRedeemInvite()
    expect(vm.inviteToken).toBe('')
  })

  it('invite token is cleared after a FAILED redemption (no lingering token in the form)', async () => {
    apiMock.redeemInvite.mockRejectedValueOnce(Object.assign(new Error('invalid'), { status: 404 }))
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    vm.inviteToken = 'bad-token'
    await vm.submitRedeemInvite()
    expect(vm.inviteToken).toBe('')
    expect(wrapper.find('[data-testid="invite-error"]').text()).toMatch(/invalid|expired|revoked/i)
  })

  it('invite token is NEVER placed in SPA/router URL, route query/history, storage, logs, or displayed messages', async () => {
    const TOKEN = 'TOPSECRET-DO-NOT-LOG'
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {})
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    apiMock.redeemInvite.mockRejectedValueOnce(Object.assign(new Error('invalid'), { status: 404 }))
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    vm.inviteToken = TOKEN
    await vm.submitRedeemInvite()

    // (a) Storage: scan EVERY localStorage + sessionStorage key/value
    // (not just the token string as a key — the token could be
    // stored as the value of another key).
    for (const store of [localStorage, sessionStorage]) {
      for (let i = 0; i < store.length; i++) {
        const key = store.key(i)
        const value = store.getItem(key)
        expect(key || '').not.toContain(TOKEN)
        expect(value || '').not.toContain(TOKEN)
      }
    }

    // (b) Router calls: every push argument serialized MUST NOT
    // contain the raw token. The encoded backend path IS allowed
    // (it is the documented API URL and is covered separately by
    // the roomApi.spec.js test) — but no SPA-side router push,
    // route query, or route params may carry the token.
    for (const call of pushMock.mock.calls) {
      for (const arg of call) {
        const flat = JSON.stringify(arg)
        expect(flat).not.toContain(TOKEN)
      }
    }

    // (c) Console logs: every log/warn/error MUST NOT contain the
    // raw token.
    for (const spy of [logSpy, warnSpy, errSpy]) {
      for (const call of spy.mock.calls) {
        const flat = call.map((a) => typeof a === 'string' ? a : JSON.stringify(a)).join(' ')
        expect(flat).not.toContain(TOKEN)
      }
    }

    // (d) Displayed messages: every toast call (error / success /
    // info) MUST NOT contain the raw token.
    for (const toastFn of [toastMock.error, toastMock.success, toastMock.info]) {
      for (const call of toastFn.mock.calls) {
        const flat = call.map((a) => typeof a === 'string' ? a : JSON.stringify(a)).join(' ')
        expect(flat).not.toContain(TOKEN)
      }
    }

    // (e) DOM-rendered text: the visible RoomEntry HTML MUST NOT
    // contain the raw token. (Encoded representations are still
    // allowed since they are the documented backend API URL shape,
    // but the raw token string itself must never appear.)
    expect(wrapper.html()).not.toContain(TOKEN)

    logSpy.mockRestore()
    warnSpy.mockRestore()
    errSpy.mockRestore()
  })

  it('invite redemption existing-member outcome (room_id present) navigates normally', async () => {
    apiMock.redeemInvite.mockResolvedValueOnce({ room_id: 1, user_id: 1, role: 'guest', joined_at: 't' })
    apiMock.listRooms.mockResolvedValue([{ id: 1, slug: 'lobby', name: 'Lobby', status: 'active' }])
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    vm.inviteToken = 'already-member-token'
    await vm.submitRedeemInvite()
    expect(pushMock).toHaveBeenCalledWith({ name: 'Room', params: { slug: 'lobby' } })
  })

  it('invite redemption with follow-up list failure surfaces a non-token success message and does NOT retry redemption', async () => {
    apiMock.listRooms
      .mockResolvedValueOnce([]) // mount
      .mockRejectedValueOnce(Object.assign(new Error('boom'), { status: 500 })) // post-redeem refresh
    apiMock.redeemInvite.mockResolvedValueOnce({ room_id: 7, user_id: 1, role: 'guest', joined_at: 't' })
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    apiMock.redeemInvite.mockClear()
    const vm = wrapper.vm
    vm.inviteToken = 'good-but-list-fails'
    await vm.submitRedeemInvite()
    // The redemption succeeded exactly once; no automatic retry.
    expect(apiMock.redeemInvite).toHaveBeenCalledTimes(1)
    // The success message does NOT echo the token.
    const successEl = wrapper.find('[data-testid="invite-success"]')
    expect(successEl.exists()).toBe(true)
    expect(successEl.text()).not.toContain('good-but-list-fails')
    // Restore the default mock for the next test.
    apiMock.listRooms.mockResolvedValue([])
  })

  it('invite redemption maps 401 / 404 / 409 / 410 to distinct error messages', async () => {
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    const expected = {
      401: /signed out/i,
      404: /invalid|expired|revoked/i,
      409: /archived/i,
      410: /used up/i,
    }
    for (const status of Object.keys(expected)) {
      apiMock.redeemInvite.mockRejectedValueOnce(Object.assign(new Error(`err ${status}`), { status: Number(status) }))
      vm.inviteToken = `tok-${status}`
      await vm.submitRedeemInvite()
      const errEl = wrapper.find('[data-testid="invite-error"]')
      expect(errEl.exists()).toBe(true)
      expect(errEl.text()).toMatch(expected[status])
    }
  })

  it('in-flight guards prevent duplicate submissions for manual-open / create / redeem', async () => {
    // Resolve the mount's listRooms immediately so the in-flight
    // guard for the next operation is the only one in play.
    apiMock.listRooms.mockResolvedValueOnce([])
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm

    // Manual open: in-flight blocks a second invocation
    apiMock.getRoom.mockImplementation(() => new Promise(() => { /* never resolves */ }))
    vm.manualSlug = 'lobby'
    const p1 = vm.openBySlug()
    const p2 = vm.openBySlug()
    await flushPromises()
    expect(apiMock.getRoom).toHaveBeenCalledTimes(1)
    // Discard the dangling promises so they don't keep the test alive.
    p1.catch(() => {})
    p2.catch(() => {})
    // Restore a fast-resolving mock and let the in-flight guard clear.
    apiMock.getRoom.mockResolvedValueOnce({ id: 1, slug: 'lobby', name: 'Lobby', status: 'active' })
    await vm.openBySlug().catch(() => {})
    await flushPromises()

    // Create room: in-flight blocks a second invocation
    apiMock.createRoom.mockImplementation(() => new Promise(() => {}))
    vm.createSlug = 'foo'
    vm.createName = 'Foo'
    const c1 = vm.submitCreateRoom()
    const c2 = vm.submitCreateRoom()
    await flushPromises()
    expect(apiMock.createRoom).toHaveBeenCalledTimes(1)
    c1.catch(() => {})
    c2.catch(() => {})
    apiMock.createRoom.mockResolvedValueOnce({ id: 1, slug: 'foo', name: 'Foo', status: 'active' })
    await vm.submitCreateRoom().catch(() => {})
    await flushPromises()

    // Redeem: in-flight blocks a second invocation
    apiMock.redeemInvite.mockImplementation(() => new Promise(() => {}))
    vm.inviteToken = 'tok'
    const r1 = vm.submitRedeemInvite()
    const r2 = vm.submitRedeemInvite()
    await flushPromises()
    expect(apiMock.redeemInvite).toHaveBeenCalledTimes(1)
    r1.catch(() => {})
    r2.catch(() => {})
    // Cleanup: replace with a resolved value so any leftover pending
    // operation from a previous scenario (e.g. the manual-open case
    // resolving through a leftover handler) does not block test exit.
    apiMock.redeemInvite.mockResolvedValue({ room_id: 1, user_id: 1, role: 'guest', joined_at: 't' })
    await vm.submitRedeemInvite().catch(() => {})
    await flushPromises()
  })

  it('Back to dashboard navigates to Dashboard', async () => {
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    await wrapper.find('[data-testid="dashboard-btn"]').trigger('click')
    expect(pushMock).toHaveBeenCalledWith({ name: 'Dashboard' })
  })

  it('RoomEntryView keeps all room-entry state LOCAL (no globalStore mutation)', async () => {
    apiMock.listRooms.mockResolvedValueOnce([
      { id: 1, slug: 'lobby', name: 'Lobby', status: 'active' },
    ])
    const before = JSON.stringify({ ...globalStore })
    const { wrapper } = mountRoomEntry()
    await flushPromises()
    const vm = wrapper.vm
    vm.manualSlug = 'lobby'
    vm.createSlug = 'new'
    vm.createName = 'New'
    vm.inviteToken = 'tok'
    await flushPromises()
    const after = JSON.stringify({ ...globalStore })
    expect(after).toBe(before)
  })
})