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
  // Single request-ownership gate. `inFlight` is the ONE flag every
  // lease network call acquires: initial GET, periodic tick, claim,
  // release, holder-loss GET, and heartbeat-403 recovery. `idleWaiters`
  // lets claim / release await the gate becoming free (as a microtask,
  // before any pending setTimeout tick) instead of racing a passive
  // request. Only ONE lifecycle request may be active at a time.
  let inFlight = false
  let idleWaiters = []
  // Generation-aware pending-tick marker. `-1` means no follow-up is owed.
  // When a visibility/online trigger — or a slug-change initial read —
  // arrives while the gate is held, this records the generation current at
  // that moment. releaseGate() then starts EXACTLY ONE immediate follow-up
  // for whatever generation is current when the gate frees, so completion
  // of an OLD-generation request still launches the NEW room's initial read.
  let pendingTick = -1
  // Dedicated destructive-submit guard so rapid Release clicks send the
  // archive request at most once.
  let releaseInFlight = false
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

  function markBusy() {
    inFlight = true
  }

  function markIdle() {
    inFlight = false
    if (idleWaiters.length > 0) {
      const waiters = idleWaiters
      idleWaiters = []
      for (const resolve of waiters) resolve()
    }
  }

  // Resolves as soon as no lease request is in flight. A caller awaiting
  // this runs as a microtask before any pending setTimeout tick, so it
  // acquires the gate (markBusy) before a passive tick can start.
  function whenIdle() {
    if (!inFlight) return Promise.resolve()
    return new Promise((resolve) => { idleWaiters.push(resolve) })
  }

  // Records that a follow-up tick is owed, tagged with the current
  // generation for context. `releaseGate` always drains it for whatever
  // generation is current when the gate frees.
  function queuePendingTick() {
    pendingTick = generation
  }

  function hasPendingTick() {
    return pendingTick >= 0
  }

  // The ONE gate-release path shared by periodic ticks, Claim, and Release.
  // Frees the request gate, then — if a trigger arrived while the gate was
  // held and the lifecycle is still live — starts exactly one immediate
  // follow-up tick for the CURRENT generation. Draining is deliberately NOT
  // gated on the completing request's own generation: after a slug change,
  // the OLD-generation request must still release the gate and launch the
  // NEW room's queued initial read. scheduleTick coalesces if a request is
  // somehow still active, so overlapping requests are impossible.
  function releaseGate() {
    markIdle()
    if (hasPendingTick() && !stopLifecycle) {
      pendingTick = -1
      scheduleTick(0)
    }
  }

  // Terminal stop used by explicit release success and by the
  // archived/removed reaction. Bumping the generation invalidates any
  // in-flight request so a late resolution cannot reapply the lease.
  function enterTerminalUnavailable() {
    stopLifecycle = true
    generation += 1
    clearTimers()
    pendingTick = -1
    lease.value = null
    state.value = 'unavailable'
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

  // Guard for actions that awaited the request gate (Claim, Release). The
  // queued action must abort if the lifecycle advanced while it waited:
  //   - the generation changed (slug change / dispose), OR
  //   - stopLifecycle was set by a terminal response that does NOT bump the
  //     generation (lease GET 409/401/403, heartbeat 409/410), OR
  //   - the active slug no longer matches the one captured at entry.
  // Without the stopLifecycle + slug checks a queued action could cross a
  // terminal boundary and act on an unavailable / stale room.
  function canProceedAfterWait(myGeneration, slug) {
    return myGeneration === generation && !stopLifecycle && currentSlug() === slug
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
      await handleHeartbeatError(myGeneration, e)
    }
  }

  async function handleHeartbeatError(myGeneration, e) {
    const status = e && e.status
    if (status === 400 || status === 401) {
      // Stop heartbeating; surface via toast.
      deps.toast.error(status === 401 ? 'You are signed out. Log in again.' : 'Invalid room reference.')
      stopLifecycle = true
      clearTimers()
    } else if (status === 403) {
      // Exit holder mode; refresh once. Awaited so the recovery GET
      // completes inside the held gate — it must not run after the tick
      // clears inFlight.
      deps.toast.error('You are no longer the lease holder.')
      await readLease(myGeneration)
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
      queuePendingTick()
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
      queuePendingTick()
      return
    }
    markBusy()
    try {
      const l = lease.value
      const uid = currentUserId()
      if (l && uid != null && Number(l.claimed_by_user_id) === Number(uid)) {
        await heartbeatIfHolder(myGeneration)
      } else {
        await readLease(myGeneration)
      }
    } finally {
      // Release through the shared helper so a queued trigger — including a
      // slug-change initial read that coalesced while THIS (possibly
      // now-stale) request was active — fires exactly one immediate
      // follow-up for the CURRENT generation.
      releaseGate()
    }
  }

  async function claim() {
    if (isClaimInFlight.value) return
    const slug = currentSlug()
    if (!slug) return
    const myGeneration = generation
    isClaimInFlight.value = true
    try {
      // Acquire the single request gate so claim serializes AFTER any
      // passive GET/heartbeat and blocks passive ticks while it runs; a
      // stale passive 404 can no longer overwrite held_by_me.
      await whenIdle()
      if (!canProceedAfterWait(myGeneration, slug)) return
      markBusy()
      try {
        const response = await api.claimRoomPlayerLease(slug)
        if (myGeneration !== generation) return
        applyLease(myGeneration, response)
        scheduleNext(myGeneration, NORMAL_INTERVAL_MS)
      } catch (e) {
        if (myGeneration !== generation) return
        const status = e && e.status
        if (status === 409) {
          // Someone else won the race; refresh once. The refresh GET runs
          // inside the held gate (no re-acquire).
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
        // Shared gate release: a visibility/online trigger that coalesced
        // during the claim fires its immediate follow-up now instead of
        // waiting for the next normal interval.
        releaseGate()
      }
    } finally {
      isClaimInFlight.value = false
    }
  }

  async function release() {
    // Destructive: at most one archive request even under rapid clicks.
    if (releaseInFlight || stopLifecycle) return
    const slug = currentSlug()
    if (!slug) return
    const myGeneration = generation
    releaseInFlight = true
    try {
      // Acquire the single request gate so no heartbeat is concurrently
      // active while the destructive release runs.
      await whenIdle()
      if (!canProceedAfterWait(myGeneration, slug)) return
      markBusy()
      try {
        await api.releaseRoomPlayerLease(slug)
        if (myGeneration !== generation) return
        // Invalidate the generation FIRST so any older in-flight heartbeat
        // that resolves 200 afterward is discarded and cannot reapply the
        // lease.
        generation += 1
        stopLifecycle = true
        clearTimers()
        state.value = 'unavailable'
        lease.value = null
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
      } finally {
        // Shared gate release. On release SUCCESS stopLifecycle is set, so
        // releaseGate does NOT start a follow-up; on a recoverable error the
        // lifecycle continues and any queued trigger fires immediately.
        releaseGate()
      }
    } finally {
      releaseInFlight = false
    }
  }

  function reset() {
    generation += 1
    stopLifecycle = true
    clearTimers()
    pendingTick = -1
    lease.value = null
    state.value = 'loading'
  }

  // Slug watcher — invalidate old generation, reset lease state, start a new initial GET.
  watch(slugRef, (newSlug, oldSlug) => {
    if (newSlug === oldSlug) return
    reset()
    stopLifecycle = false
    releaseInFlight = false
    state.value = 'loading'
    const myGeneration = generation
    if (!newSlug) return
    if (deps.isRoomDisabled && deps.isRoomDisabled.value) {
      enterTerminalUnavailable()
      return
    }
    // Route the initial read through runOneTick so it is single-flighted.
    runOneTick(myGeneration)
  })

  // React to the room becoming archived/removed. The flag is owned by
  // RoomView and flips on the R10b WebSocket envelope. Immediately stop
  // the heartbeat, clear the lease, and enter the terminal unavailable
  // state instead of waiting for the next REST failure.
  if (deps.isRoomDisabled) {
    watch(deps.isRoomDisabled, (disabled) => {
      if (disabled && !stopLifecycle) enterTerminalUnavailable()
    })
  }

  // Initial kick.
  attachListeners()
  generation += 1
  state.value = 'loading'
  if (deps.isRoomDisabled && deps.isRoomDisabled.value) {
    enterTerminalUnavailable()
  } else if (currentSlug()) {
    // Route the initial read through runOneTick so it is single-flighted;
    // an immediate visibility/online trigger coalesces into pendingTick
    // instead of firing a second concurrent GET.
    runOneTick(generation)
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