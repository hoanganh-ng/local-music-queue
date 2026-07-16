import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { ref, computed, nextTick } from 'vue'
import { useRoomPlayerLease } from '../useRoomPlayerLease'

const { apiMock, toastMock } = vi.hoisted(() => {
  const apiMock = {
    claimRoomPlayerLease: vi.fn(),
    heartbeatRoomPlayerLease: vi.fn(),
    releaseRoomPlayerLease: vi.fn(),
    getRoomPlayerLease: vi.fn(),
    getRoom: vi.fn(),
  }
  const toastMock = {
    success: vi.fn(),
    error: vi.fn(),
    info: vi.fn(),
  }
  return { apiMock, toastMock }
})

vi.mock('../../services/api', () => ({ api: apiMock }))

function makeDeps(overrides = {}) {
  const slugRef = ref('lobby')
  const currentUser = ref({ id: 1, display_name: 'Me' })
  const isRoomHost = computed(() => true)
  const isConnected = computed(() => true)
  const isRoomDisabled = computed(() => false)
  const onArchived = vi.fn()
  return {
    slugRef,
    deps: { currentUser, isRoomHost, isConnected, isRoomDisabled, onArchived, toast: toastMock },
    ...overrides,
  }
}

describe('useRoomPlayerLease', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
    sessionStorage.clear()
    vi.resetAllMocks()
    // Default: GET returns no lease (404).
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('no lease'), { status: 404 }))
    apiMock.claimRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.heartbeatRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't2', expires_at: 't2' })
    apiMock.releaseRoomPlayerLease.mockResolvedValue(null)
    apiMock.getRoom.mockResolvedValue({ id: 7, slug: 'lobby', status: 'archived' })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  // Drain pending microtasks AND fire any due timers. With vi.useFakeTimers
  // + mockResolvedValue, the microtask queue holds the awaited API responses
  // but vi.runOnlyPendingTimersAsync alone doesn't flush them. vi.advanceTimersByTimeAsync(0)
  // advances time by 0 ms while ALSO draining the microtask queue, which
  // is exactly what we need to observe a single state-mutating cycle.
  async function settle() {
    await vi.advanceTimersByTimeAsync(0)
  }

  it('initial state is loading until the first GET resolves', async () => {
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    expect(lease.state.value).toBe('loading')
    await settle()
    expect(apiMock.getRoomPlayerLease).toHaveBeenCalledWith('lobby')
  })

  it('initial GET resolving with no lease (404) sets state to none', async () => {
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    expect(lease.state.value).toBe('none')
    expect(lease.lease.value).toBeNull()
  })

  it('initial GET held by current user sets state to held_by_me', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    expect(lease.lease.value.claimed_by_user_id).toBe(1)
  })

  it('initial GET held by another user sets state to held_by_other', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 2, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    expect(lease.state.value).toBe('held_by_other')
    expect(lease.lease.value.claimed_by_user_id).toBe(2)
  })

  it('a non-holder never heartbeats — every tick is a GET', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 2, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    useRoomPlayerLease(slugRef, deps)
    await settle()
    apiMock.getRoomPlayerLease.mockClear()
    vi.advanceTimersByTime(20_000)
    await settle()
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(apiMock.getRoomPlayerLease.mock.calls.length).toBeGreaterThanOrEqual(2)
    expect(apiMock.heartbeatRoomPlayerLease).not.toHaveBeenCalled()
  })

  it('the current holder heartbeats every 20 seconds', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    useRoomPlayerLease(slugRef, deps)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(0)
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(2)
  })

  it('claim success begins the holder cadence and sets state to held_by_me', async () => {
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('no lease'), { status: 404 }))
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    expect(lease.state.value).toBe('none')
    apiMock.claimRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    await lease.claim()
    expect(lease.state.value).toBe('held_by_me')
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(1)
  })

  it('transient failures retry after 5 seconds without toast spam', async () => {
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('boom'), { status: 500 }))
    const { slugRef, deps } = makeDeps()
    useRoomPlayerLease(slugRef, deps)
    await settle()
    const initialCalls = apiMock.getRoomPlayerLease.mock.calls.length
    const initialToasts = toastMock.error.mock.calls.length
    vi.advanceTimersByTime(5_000)
    await settle()
    expect(apiMock.getRoomPlayerLease.mock.calls.length).toBeGreaterThan(initialCalls)
    // Transient retries are silent — only terminal failures toast.
    expect(toastMock.error.mock.calls.length).toBe(initialToasts)
  })

  it('heartbeat 403 exits holder mode and refreshes the lease once', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.heartbeatRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('forbidden'), { status: 403 }))
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 2, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(lease.state.value).toBe('held_by_other')
  })

  it('heartbeat 404 exits holder mode and sets state to none', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.heartbeatRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('not found'), { status: 404 }))
    // After holder-mode exit, the composable calls GET; default 404 mock fires.
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(lease.state.value).toBe('none')
  })

  it('heartbeat 409 marks the room archived locally', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.heartbeatRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('archived'), { status: 409 }))
    const { slugRef, deps } = makeDeps()
    useRoomPlayerLease(slugRef, deps)
    await settle()
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(deps.onArchived).toHaveBeenCalled()
  })

  it('heartbeat 410 stops heartbeat and enters expired_pending_archive', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.heartbeatRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('gone'), { status: 410 }))
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(lease.state.value).toBe('expired_pending_archive')
    // Heartbeats must stop — advancing again should not fire another.
    apiMock.heartbeatRoomPlayerLease.mockClear()
    vi.advanceTimersByTime(60_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).not.toHaveBeenCalled()
  })

  it('hidden state never calls release', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    useRoomPlayerLease(slugRef, deps)
    await settle()
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    vi.advanceTimersByTime(60_000)
    await settle()
    expect(apiMock.releaseRoomPlayerLease).not.toHaveBeenCalled()
  })

  it('visibility → visible triggers an immediate tick', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    useRoomPlayerLease(slugRef, deps)
    await settle()
    apiMock.heartbeatRoomPlayerLease.mockClear()
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await settle()
    // The immediate tick should have triggered a heartbeat.
    expect(apiMock.heartbeatRoomPlayerLease.mock.calls.length).toBeGreaterThanOrEqual(1)
  })

  it('unmount (dispose) clears timers and listeners', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    lease.dispose()
    apiMock.getRoomPlayerLease.mockClear()
    apiMock.heartbeatRoomPlayerLease.mockClear()
    vi.advanceTimersByTime(60_000)
    await settle()
    expect(apiMock.getRoomPlayerLease).not.toHaveBeenCalled()
    expect(apiMock.heartbeatRoomPlayerLease).not.toHaveBeenCalled()
  })

  it('slug change clears the prior lease; a stale old-slug response cannot mutate the new room', async () => {
    // Hold the initial GET as an unresolved Promise so it stays "in flight"
    // while we navigate to a new slug. After the new slug, we resolve the
    // OLD-slug Promise and verify it cannot mutate the new room.
    let resolveOldGet
    apiMock.getRoomPlayerLease.mockImplementationOnce(() => new Promise((r) => { resolveOldGet = r }))

    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    // Yield once so the GET is actually issued; leave it unresolved.
    await settle()
    expect(lease.state.value).toBe('loading')

    // Navigate to the new slug while the OLD GET is still in flight.
    slugRef.value = 'lounge'
    await nextTick()

    // The new slug's GET is also in flight (default 404 mock returns a
    // rejected Promise; we let it sit). The composable has reset state.
    // Now resolve the OLD-slug GET with a stale payload. The generation
    // token MUST prevent it from landing on the new room.
    resolveOldGet({ id: 99, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    await settle()

    // The stale old-slug payload MUST NOT have re-applied to the new room.
    if (lease.lease.value) {
      expect(lease.lease.value.id).not.toBe(99)
    }
  })

  it('explicit release on 204 stops lifecycle and marks archived locally', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.releaseRoomPlayerLease.mockResolvedValue(null)
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    await lease.release()
    expect(deps.onArchived).toHaveBeenCalled()
    // After release, no further heartbeats fire.
    apiMock.heartbeatRoomPlayerLease.mockClear()
    vi.advanceTimersByTime(60_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).not.toHaveBeenCalled()
  })

  it('release 404 does NOT mark archived', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.releaseRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('no lease'), { status: 404 }))
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    await lease.release()
    expect(deps.onArchived).not.toHaveBeenCalled()
  })

  it('claim 409 refreshes the lease once; refresh 200 applies the new holder', async () => {
    // Queue order for getRoomPlayerLease: initial GET rejects 404, then
    // the post-409 refresh resolves with another holder.
    apiMock.getRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('no lease'), { status: 404 }))
    apiMock.claimRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('lost race'), { status: 409 }))
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 2, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    expect(lease.state.value).toBe('none')
    await lease.claim()
    await settle()
    expect(lease.state.value).toBe('held_by_other')
  })

  it('claim 409 with refresh 404 surfaces a generic conflict toast without inventing a holder', async () => {
    // Initial GET 404 → state=none.
    // Claim 409 → refresh GET 404 → generic conflict toast, no holder.
    apiMock.getRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('no lease'), { status: 404 }))
    apiMock.claimRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('lost race'), { status: 409 }))
    apiMock.getRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('still no lease'), { status: 404 }))
    const { slugRef, deps } = makeDeps()
    const lease = useRoomPlayerLease(slugRef, deps)
    await settle()
    await lease.claim()
    await settle()
    expect(toastMock.error).toHaveBeenCalled()
    expect(toastMock.error.mock.calls.some(c => /conflict with another holder/i.test(c[0]))).toBe(true)
    expect(lease.lease.value).toBeNull()
  })
})