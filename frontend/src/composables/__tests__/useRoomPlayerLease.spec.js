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
  // Track every composable created in a test so afterEach disposes it.
  // Without this, a composable's global visibilitychange/online listeners
  // leak across tests and fire extra requests against the shared apiMock
  // when a later test dispatches those global events.
  let createdLeases = []
  function trackLease(l) {
    createdLeases.push(l)
    return l
  }

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
    // Dispose every tracked composable so its listeners/timers cannot leak
    // into the next test.
    for (const l of createdLeases) {
      try { l.dispose() } catch (e) { /* ignore */ }
    }
    createdLeases = []
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    expect(lease.state.value).toBe('loading')
    await settle()
    expect(apiMock.getRoomPlayerLease).toHaveBeenCalledWith('lobby')
  })

  it('initial GET resolving with no lease (404) sets state to none', async () => {
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(lease.state.value).toBe('none')
    expect(lease.lease.value).toBeNull()
  })

  it('initial GET held by current user sets state to held_by_me', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    expect(lease.lease.value.claimed_by_user_id).toBe(1)
  })

  it('initial GET held by another user sets state to held_by_other', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 2, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(lease.state.value).toBe('held_by_other')
    expect(lease.lease.value.claimed_by_user_id).toBe(2)
  })

  it('a non-holder never heartbeats — every tick is a GET', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 2, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    trackLease(useRoomPlayerLease(slugRef, deps))
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
    trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(deps.onArchived).toHaveBeenCalled()
  })

  it('heartbeat 410 stops heartbeat and enters expired_pending_archive', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.heartbeatRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('gone'), { status: 410 }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    trackLease(useRoomPlayerLease(slugRef, deps))
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
    trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
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
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    await lease.claim()
    await settle()
    expect(toastMock.error).toHaveBeenCalled()
    expect(toastMock.error.mock.calls.some(c => /conflict with another holder/i.test(c[0]))).toBe(true)
    expect(lease.lease.value).toBeNull()
  })

  // --- R05b2 corrective pass: single-flight request-ownership races ---

  it('initial GET is single-flighted — an immediate visibility trigger fires no second concurrent GET', async () => {
    let resolveInitial
    apiMock.getRoomPlayerLease.mockImplementationOnce(() => new Promise((r) => { resolveInitial = r }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(apiMock.getRoomPlayerLease).toHaveBeenCalledTimes(1)
    // Fire visibility while the initial GET is still in flight.
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await settle()
    // The trigger coalesced — still exactly one in-flight GET.
    expect(apiMock.getRoomPlayerLease).toHaveBeenCalledTimes(1)
    resolveInitial({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    lease.dispose()
  })

  it('a passive GET in flight cannot overwrite a concurrent Claim — claim wins', async () => {
    let rejectGet
    apiMock.getRoomPlayerLease.mockImplementationOnce(() => new Promise((_res, rej) => { rejectGet = rej }))
    apiMock.claimRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    // Passive GET is in flight; request a claim while it runs.
    const claimP = lease.claim()
    await settle()
    // Claim must NOT have fired yet — it waits for the gate.
    expect(apiMock.claimRoomPlayerLease).not.toHaveBeenCalled()
    // The passive GET resolves as no-lease (404) — the stale outcome.
    rejectGet(Object.assign(new Error('no lease'), { status: 404 }))
    await settle()
    await claimP
    await settle()
    expect(apiMock.claimRoomPlayerLease).toHaveBeenCalledTimes(1)
    expect(lease.state.value).toBe('held_by_me')
    expect(lease.lease.value.claimed_by_user_id).toBe(1)
    lease.dispose()
  })

  it('Release wins over a concurrent heartbeat and the lease is not reapplied afterward', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    let resolveHb
    apiMock.heartbeatRoomPlayerLease.mockImplementationOnce(() => new Promise((r) => { resolveHb = r }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    // Start a heartbeat and leave it in flight.
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(1)
    // Release while the heartbeat is still in flight.
    const relP = lease.release()
    await settle()
    // Heartbeat resolves 200 (would reapply the lease pre-fix).
    resolveHb({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't2', expires_at: 't2' })
    await settle()
    await relP
    await settle()
    expect(lease.state.value).toBe('unavailable')
    expect(deps.onArchived).toHaveBeenCalled()
    // No further heartbeat and the lease stays cleared.
    apiMock.heartbeatRoomPlayerLease.mockClear()
    vi.advanceTimersByTime(60_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).not.toHaveBeenCalled()
    expect(lease.lease.value).toBeNull()
    lease.dispose()
  })

  it('rapid Release clicks submit the destructive archive request at most once', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    let resolveRel
    apiMock.releaseRoomPlayerLease.mockImplementationOnce(() => new Promise((r) => { resolveRel = r }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    const p1 = lease.release()
    const p2 = lease.release()
    await settle()
    resolveRel(null)
    await Promise.all([p1, p2])
    await settle()
    expect(apiMock.releaseRoomPlayerLease).toHaveBeenCalledTimes(1)
    expect(lease.state.value).toBe('unavailable')
    lease.dispose()
  })

  it('an online event triggers an immediate recovery tick', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    apiMock.heartbeatRoomPlayerLease.mockClear()
    window.dispatchEvent(new Event('online'))
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease.mock.calls.length).toBeGreaterThanOrEqual(1)
    lease.dispose()
  })

  it('a trigger during an active request produces one immediate follow-up (0ms, not 20s)', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    let resolveHb
    apiMock.heartbeatRoomPlayerLease.mockImplementationOnce(() => new Promise((r) => { resolveHb = r }))
    apiMock.heartbeatRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't2', expires_at: 't2' })
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(1)
    // Trigger while the heartbeat is in flight — coalesces into pendingTick.
    window.dispatchEvent(new Event('online'))
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(1)
    // Resolve the in-flight heartbeat; the follow-up fires immediately
    // (0ms) WITHOUT advancing a full 20s interval.
    resolveHb({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't2', expires_at: 't2' })
    await settle()
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(2)
    lease.dispose()
  })

  it('410 enters expired_pending_archive, polls, and confirms archive completion', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    apiMock.heartbeatRoomPlayerLease.mockRejectedValueOnce(Object.assign(new Error('gone'), { status: 410 }))
    apiMock.getRoom.mockResolvedValue({ id: 7, slug: 'lobby', status: 'archived' })
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(lease.state.value).toBe('expired_pending_archive')
    // Archive poll runs after 5s and confirms the room is archived.
    vi.advanceTimersByTime(5_000)
    await settle()
    expect(lease.state.value).toBe('unavailable')
    expect(deps.onArchived).toHaveBeenCalled()
    lease.dispose()
  })

  it('after dispose, visibility/online events trigger no API calls', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    lease.dispose()
    apiMock.getRoomPlayerLease.mockClear()
    apiMock.heartbeatRoomPlayerLease.mockClear()
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event('online'))
    vi.advanceTimersByTime(60_000)
    await settle()
    expect(apiMock.getRoomPlayerLease).not.toHaveBeenCalled()
    expect(apiMock.heartbeatRoomPlayerLease).not.toHaveBeenCalled()
  })

  it('isRoomDisabled flipping true immediately enters terminal unavailable and stops heartbeats', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    const disabled = ref(false)
    const slugRef = ref('lobby')
    const currentUser = ref({ id: 1, display_name: 'Me' })
    const deps = {
      currentUser,
      isRoomHost: computed(() => true),
      isConnected: computed(() => true),
      isRoomDisabled: computed(() => disabled.value),
      onArchived: vi.fn(),
      toast: toastMock,
    }
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    disabled.value = true
    await nextTick()
    expect(lease.state.value).toBe('unavailable')
    expect(lease.lease.value).toBeNull()
    apiMock.heartbeatRoomPlayerLease.mockClear()
    vi.advanceTimersByTime(60_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).not.toHaveBeenCalled()
    lease.dispose()
  })

  // --- R05b2 corrective pass: generation-aware pending-tick drain ---

  it('a slug change during an in-flight old-slug GET starts exactly one new-room GET after the old request releases the gate', async () => {
    // Old-slug initial GET stays in flight across the slug change.
    let resolveOldGet
    apiMock.getRoomPlayerLease.mockImplementationOnce(() => new Promise((r) => { resolveOldGet = r }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(apiMock.getRoomPlayerLease).toHaveBeenCalledTimes(1)
    expect(apiMock.getRoomPlayerLease).toHaveBeenLastCalledWith('lobby')
    expect(lease.state.value).toBe('loading')

    // Navigate to the new slug while the OLD GET is still in flight. The
    // new-room read must coalesce into the generation-aware pending tick
    // rather than fire a second concurrent GET.
    slugRef.value = 'lounge'
    await nextTick()
    expect(apiMock.getRoomPlayerLease).toHaveBeenCalledTimes(1)

    // The new-room GET (issued once the gate frees) resolves held_by_me.
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 5, room_id: 9, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    // Resolving the stale OLD-slug GET releases the gate; completion of the
    // old generation MUST launch the queued NEW-room read for the current
    // generation (pre-fix this was dropped and the room stuck in loading).
    resolveOldGet({ id: 99, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    await settle()
    await settle()

    // Exactly one new-room GET fired, against the NEW slug, and the new room
    // reached a resolved lease state.
    expect(apiMock.getRoomPlayerLease).toHaveBeenCalledTimes(2)
    expect(apiMock.getRoomPlayerLease).toHaveBeenLastCalledWith('lounge')
    expect(lease.state.value).toBe('held_by_me')
    expect(lease.lease.value.id).toBe(5)
    lease.dispose()
  })

  it('a slug change during an in-flight heartbeat starts the new-room GET after the heartbeat releases the gate', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    let resolveHb
    apiMock.heartbeatRoomPlayerLease.mockImplementationOnce(() => new Promise((r) => { resolveHb = r }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    // Start a heartbeat and leave it in flight.
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(1)

    // Navigate away while the heartbeat is still in flight.
    apiMock.getRoomPlayerLease.mockClear()
    apiMock.getRoomPlayerLease.mockResolvedValueOnce({ id: 5, room_id: 9, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    slugRef.value = 'lounge'
    await nextTick()
    // The new-room read coalesced behind the in-flight heartbeat.
    expect(apiMock.getRoomPlayerLease).not.toHaveBeenCalled()

    // The stale heartbeat resolves; releasing the gate launches the queued
    // new-room initial read for the current generation.
    resolveHb({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't2', expires_at: 't2' })
    await settle()
    await settle()
    expect(apiMock.getRoomPlayerLease).toHaveBeenCalledTimes(1)
    expect(apiMock.getRoomPlayerLease).toHaveBeenLastCalledWith('lounge')
    expect(lease.state.value).toBe('held_by_me')
    expect(lease.lease.value.id).toBe(5)
    lease.dispose()
  })

  it('an online event during Claim produces exactly one immediate post-Claim tick', async () => {
    apiMock.getRoomPlayerLease.mockRejectedValue(Object.assign(new Error('no lease'), { status: 404 }))
    let resolveClaim
    apiMock.claimRoomPlayerLease.mockImplementationOnce(() => new Promise((r) => { resolveClaim = r }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(lease.state.value).toBe('none')

    const claimP = lease.claim()
    await settle()
    expect(apiMock.claimRoomPlayerLease).toHaveBeenCalledTimes(1)

    // Online fires WHILE the claim holds the gate — it must coalesce into
    // the pending tick, not run a second concurrent request.
    apiMock.heartbeatRoomPlayerLease.mockClear()
    window.dispatchEvent(new Event('online'))
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).not.toHaveBeenCalled()

    // Claim resolves held_by_me; releasing the gate fires exactly one
    // immediate follow-up tick (a heartbeat) instead of waiting for the 20s
    // normal timer (pre-fix Claim's markIdle did not drain the pending tick).
    resolveClaim({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    await claimP
    await settle()
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(1)
    lease.dispose()
  })

  // --- R05b2 corrective pass: queued actions must not cross a terminal boundary ---

  it('a Claim queued behind a passive GET aborts if that GET reports the room archived (409)', async () => {
    // Initial GET stays in flight so a claim can queue behind it.
    let rejectGet
    apiMock.getRoomPlayerLease.mockImplementationOnce(() => new Promise((_res, rej) => { rejectGet = rej }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    const claimP = lease.claim()
    await settle()
    // Claim waits for the gate; it must not have fired yet.
    expect(apiMock.claimRoomPlayerLease).not.toHaveBeenCalled()
    // The passive GET reports the room archived — terminal (stopLifecycle set
    // WITHOUT a generation bump).
    rejectGet(Object.assign(new Error('archived'), { status: 409 }))
    await settle()
    await claimP
    await settle()
    // The queued claim must NOT cross the terminal boundary.
    expect(apiMock.claimRoomPlayerLease).not.toHaveBeenCalled()
    expect(lease.state.value).toBe('unavailable')
    lease.dispose()
  })

  it('a Release queued behind a heartbeat aborts if that heartbeat reports 410 (expired/archiving)', async () => {
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 1, room_id: 7, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    let rejectHb
    apiMock.heartbeatRoomPlayerLease.mockImplementationOnce(() => new Promise((_res, rej) => { rejectHb = rej }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    expect(lease.state.value).toBe('held_by_me')
    // Start a heartbeat and leave it in flight.
    vi.advanceTimersByTime(20_000)
    await settle()
    expect(apiMock.heartbeatRoomPlayerLease).toHaveBeenCalledTimes(1)
    // Queue a release behind the in-flight heartbeat.
    const relP = lease.release()
    await settle()
    expect(apiMock.releaseRoomPlayerLease).not.toHaveBeenCalled()
    // The heartbeat reports 410 — terminal (stopLifecycle set WITHOUT a
    // generation bump).
    rejectHb(Object.assign(new Error('gone'), { status: 410 }))
    await settle()
    await relP
    await settle()
    // The queued release must NOT cross the terminal boundary.
    expect(apiMock.releaseRoomPlayerLease).not.toHaveBeenCalled()
    expect(lease.state.value).toBe('expired_pending_archive')
    lease.dispose()
  })

  it('stale pending work in a terminal room fires no extra request after a later slug change', async () => {
    // Initial GET stays in flight so a trigger can coalesce into pendingTick.
    let rejectGet
    apiMock.getRoomPlayerLease.mockImplementationOnce(() => new Promise((_res, rej) => { rejectGet = rej }))
    const { slugRef, deps } = makeDeps()
    const lease = trackLease(useRoomPlayerLease(slugRef, deps))
    await settle()
    // A trigger arrives mid-request — recorded as pending work.
    window.dispatchEvent(new Event('online'))
    await settle()
    // The initial GET reports the room archived — terminal, so the pending
    // tick cannot drain (stopLifecycle) and is NOT bumped by a generation.
    rejectGet(Object.assign(new Error('archived'), { status: 409 }))
    await settle()
    expect(lease.state.value).toBe('unavailable')

    // Later slug change. reset() must clear the stale pending tick so the new
    // room issues exactly ONE initial GET — not an extra coalesced one.
    apiMock.getRoomPlayerLease.mockResolvedValue({ id: 5, room_id: 9, claimed_by_user_id: 1, claimed_at: 't', last_heartbeat_at: 't', expires_at: 't' })
    slugRef.value = 'lounge'
    await nextTick()
    await settle()
    await settle()
    const loungeCalls = apiMock.getRoomPlayerLease.mock.calls.filter((c) => c[0] === 'lounge')
    expect(loungeCalls.length).toBe(1)
    expect(lease.state.value).toBe('held_by_me')
    lease.dispose()
  })
})