import { ref, watch, onScopeDispose } from 'vue'
import { api } from '../services/api'

// R05b2: Player-lease UI and heartbeat lifecycle composable.
//
// Sole owner of:
//   - the current lease object + presentation state;
//   - initial and periodic lease reads;
//   - claim, heartbeat, and release operations;
//   - normal (20s) and retry (5s) timers;
//   - visibilitychange and online listeners;
//   - single-flight request guards;
//   - slug / mount generation protection (stale responses cannot
//     mutate the new room);
//   - terminal cleanup on unmount and slug change.
//
// State machine:
//   loading → none | held_by_me | held_by_other | unavailable
//   none → held_by_me (after successful claim)
//   held_by_me → held_by_other (after holder-loss GET on 403)
//   held_by_me → none (after holder-loss GET on 404)
//   held_by_me → retrying (network failure or 5xx)
//   held_by_me → expired_pending_archive (410)
//   {any} → retrying (transient failure)
//
// Lease state is LOCAL to the active RoomView. It is NOT persisted
// in localStorage / sessionStorage / Pinia / a global store slice.
// Timer ownership is owned by this composable only.

const NORMAL_INTERVAL_MS = 20_000
const RETRY_INTERVAL_MS = 5_000
const ARCHIVE_POLL_INTERVAL_MS = 5_000

export function useRoomPlayerLease(slugRef, deps) {
  const state = ref('loading')
  const lease = ref(null)
  const isClaimInFlight = ref(false)

  // Generation token. Bumped on slug change and on dispose. Every
  // async path captures the current generation at start; a stale
  // resolution that lands after the token has advanced is a no-op.
  let generation = 0

  // Timers + single-flight guards.
  let timer = null
  let archiveTimer = null
  let inFlight = false
  let pendingTick = false
  let stopLifecycle = false
  let listenersAttached = false

  // Bound listeners (kept as named refs so we can remove them).
  const onVisibility = () => {
    if (typeof document !== 'undefined' && document.visibilityState === 'visible') {
      scheduleTick(0)
    }
  }
  const onOnline = () => {
    scheduleTick(0)
  }

  function clearTimers() {
    if (timer != null) {
      clearTimeout(timer)
      timer = null
    }
    if (archiveTimer != null) {
      clearTimeout(archiveTimer)
      archiveTimer = null
    }
  }

  function attachListeners() {
    if (listenersAttached || typeof document === 'undefined') return
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('online', onOnline)
    listenersAttached = true
  }

  function detachListeners() {
    if (!listenersAttached || typeof document === 'undefined') return
    document.removeEventListener('visibilitychange', onVisibility)
    window.removeEventListener('online', onOnline)
    listenersAttached = false
  }

  function applyLease(myGeneration, response) {
    if (myGeneration !== generation) return
    lease.value = response || null
    const uid = currentUserId()
    const heldByMe = response && uid != null
      && Number(response.claimed_by_user_id) === Number(uid)
    if (!response) {
      state.value = 'none'
    } else if (heldByMe) {
      state.value = 'held_by_me'
    } else {
      state.value = 'held_by_other'
    }
  }

  function currentUserId() {
    const u = deps.currentUser && deps.currentUser.value
    return u && u.id != null ? Number(u.id) : null
  }

  function currentSlug() {
    return slugRef && slugRef.value != null ? String(slugRef.value) : null
  }

  async function readLease(myGeneration) {
    const slug = currentSlug()
    if (!slug) return
    try {
      const response = await api.getRoomPlayerLease(slug)
      if (myGeneration !== generation) return
      applyLease(myGeneration, response)
      scheduleNext(myGeneration, NORMAL_INTERVAL_MS)
    } catch (e) {
      if (myGeneration !== generation) return
      handleGetError(myGeneration, e)
    }
  }

  function handleGetError(myGeneration, e) {
    const status = e && e.status
    if (status === 404) {
      lease.value = null
      state.value = 'none'
      scheduleNext(myGeneration, NORMAL_INTERVAL_MS)
    } else if (status === 409) {
      // Archived.
      state.value = 'unavailable'
      lease.value = null
      stopLifecycle = true
      clearTimers()
    } else if (status === 401 || status === 403) {
      state.value = 'unavailable'
      lease.value = null
      stopLifecycle = true
      clearTimers()
    } else {
      state.value = 'retrying'
      scheduleNext(myGeneration, RETRY_INTERVAL_MS)
    }
  }

  async function heartbeatIfHolder(myGeneration) {
    const slug = currentSlug()
    const l = lease.value
    const uid = currentUserId()
    if (!slug || !l || uid == null) return
    if (Number(l.claimed_by_user_id) !== Number(uid)) {
      // We are NOT the holder — refresh with GET.
      await readLease(myGeneration)
      return
    }
    try {
      const response = await api.heartbeatRoomPlayerLease(slug)
      if (myGeneration !== generation) return
      applyLease(myGeneration, response)
      scheduleNext(myGeneration, NORMAL_INTERVAL_MS)
    } catch (e) {
      if (myGeneration !== generation) return
      handleHeartbeatError(myGeneration, e)
    }
  }

  function handleHeartbeatError(myGeneration, e) {
    const status = e && e.status
    if (status === 400 || status === 401) {
      // Stop heartbeating; surface via toast.
      deps.toast.error(status === 401 ? 'You are signed out. Log in again.' : 'Invalid room reference.')
      stopLifecycle = true
      clearTimers()
    } else if (status === 403) {
      // Exit holder mode; refresh once.
      deps.toast.error('You are no longer the lease holder.')
      readLease(myGeneration)
    } else if (status === 404) {
      lease.value = null
      state.value = 'none'
      scheduleNext(myGeneration, NORMAL_INTERVAL_MS)
    } else if (status === 409) {
      // Archived.
      state.value = 'unavailable'
      lease.value = null
      stopLifecycle = true
      clearTimers()
      if (deps.onArchived) deps.onArchived()
    } else if (status === 410) {
      state.value = 'expired_pending_archive'
      lease.value = null
      stopLifecycle = true
      clearTimers()
      deps.toast.error('Player lease has expired — the room is being archived.')
      startArchivePoll(myGeneration)
    } else {
      state.value = 'retrying'
      scheduleNext(myGeneration, RETRY_INTERVAL_MS)
    }
  }

  function startArchivePoll(myGeneration) {
    if (archiveTimer != null) clearTimeout(archiveTimer)
    const tick = async () => {
      if (myGeneration !== generation) return
      const slug = currentSlug()
      if (!slug) return
      try {
        const room = await api.getRoom(slug)
        if (myGeneration !== generation) return
        if (room && room.status !== 'active') {
          state.value = 'unavailable'
          if (deps.onArchived) deps.onArchived()
          return
        }
      } catch (e) {
        if (myGeneration !== generation) return
        // Continue polling.
      }
      if (archiveTimer != null) clearTimeout(archiveTimer)
      archiveTimer = setTimeout(tick, ARCHIVE_POLL_INTERVAL_MS)
    }
    archiveTimer = setTimeout(tick, ARCHIVE_POLL_INTERVAL_MS)
  }

  function scheduleNext(myGeneration, delayMs) {
    if (stopLifecycle) return
    if (myGeneration !== generation) return
    if (timer != null) clearTimeout(timer)
    timer = setTimeout(async () => {
      if (myGeneration !== generation) return
      if (stopLifecycle) return
      if (state.value === 'expired_pending_archive' || state.value === 'unavailable') return
      await runOneTick(myGeneration)
    }, delayMs)
  }

  function scheduleTick(delayMs) {
    // Visibility/online-driven immediate tick. If a tick is already
    // in flight, just remember we owe one follow-up tick.
    if (inFlight) {
      pendingTick = true
      return
    }
    const myGeneration = generation
    if (timer != null) clearTimeout(timer)
    timer = setTimeout(async () => {
      await runOneTick(myGeneration)
    }, delayMs)
  }

  async function runOneTick(myGeneration) {
    if (myGeneration !== generation) return
    if (stopLifecycle) return
    if (inFlight) {
      pendingTick = true
      return
    }
    inFlight = true
    try {
      const l = lease.value
      const uid = currentUserId()
      if (l && uid != null && Number(l.claimed_by_user_id) === Number(uid)) {
        await heartbeatIfHolder(myGeneration)
      } else {
        await readLease(myGeneration)
      }
    } finally {
      inFlight = false
      if (pendingTick && !stopLifecycle && myGeneration === generation) {
        pendingTick = false
        // One follow-up tick after the in-flight request resolved.
        scheduleTick(NORMAL_INTERVAL_MS)
      }
    }
  }

  async function claim() {
    if (isClaimInFlight.value) return
    const slug = currentSlug()
    if (!slug) return
    isClaimInFlight.value = true
    const myGeneration = generation
    try {
      const response = await api.claimRoomPlayerLease(slug)
      if (myGeneration !== generation) return
      applyLease(myGeneration, response)
      scheduleNext(myGeneration, NORMAL_INTERVAL_MS)
    } catch (e) {
      if (myGeneration !== generation) return
      const status = e && e.status
      if (status === 409) {
        // Someone else won the race; refresh once.
        try {
          const fresh = await api.getRoomPlayerLease(slug)
          if (myGeneration !== generation) return
          applyLease(myGeneration, fresh)
        } catch (innerErr) {
          if (myGeneration !== generation) return
          const s = innerErr && innerErr.status
          if (s === 409) {
            state.value = 'unavailable'
            if (deps.onArchived) deps.onArchived()
          } else if (s === 404) {
            state.value = 'none'
            deps.toast.error('Claim is in conflict with another holder.')
          } else {
            deps.toast.error('Could not claim the player lease.')
          }
        }
      } else if (status === 403) {
        deps.toast.error('Only the host can claim the player.')
      } else if (status === 401) {
        deps.toast.error('You are signed out. Log in again.')
      } else {
        deps.toast.error('Could not claim the player lease.')
      }
    } finally {
      isClaimInFlight.value = false
    }
  }

  async function release() {
    const slug = currentSlug()
    if (!slug) return
    const myGeneration = generation
    try {
      await api.releaseRoomPlayerLease(slug)
      if (myGeneration !== generation) return
      state.value = 'unavailable'
      lease.value = null
      stopLifecycle = true
      clearTimers()
      if (deps.onArchived) deps.onArchived()
      deps.toast.success(`Room "${slug}" archived.`)
    } catch (e) {
      if (myGeneration !== generation) return
      const status = e && e.status
      if (status === 404) {
        // No active lease; the backend defines this as no mutation,
        // no broadcast. Do NOT mark archived locally.
        deps.toast.error('No active lease to release.')
      } else if (status === 401) {
        deps.toast.error('You are signed out. Log in again.')
      } else if (status === 403) {
        deps.toast.error('Only the host can release the player.')
      } else if (status === 409) {
        deps.toast.error('Room is archived.')
      } else {
        deps.toast.error('Could not release the player.')
      }
    }
  }

  function reset() {
    generation += 1
    stopLifecycle = true
    clearTimers()
    lease.value = null
    state.value = 'loading'
  }

  // Slug watcher — invalidate old generation, reset lease state, start a new initial GET.
  watch(slugRef, (newSlug, oldSlug) => {
    if (newSlug === oldSlug) return
    reset()
    stopLifecycle = false
    state.value = 'loading'
    const myGeneration = generation
    if (!newSlug) return
    readLease(myGeneration)
  })

  // Initial kick.
  attachListeners()
  generation += 1
  state.value = 'loading'
  if (currentSlug()) {
    readLease(generation)
  } else {
    state.value = 'none'
  }

  function dispose() {
    reset()
    detachListeners()
  }

  // Vue lifecycle: dispose on scope dispose (covers component unmount).
  try {
    onScopeDispose(() => {
      dispose()
    })
  } catch (e) {
    // Non-Vue callers (tests) manage disposal manually via dispose().
  }

  return { state, lease, isClaimInFlight, claim, release, dispose }
}