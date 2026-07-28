import { describe, it, expect, beforeEach, vi } from 'vitest'

// R14d: this file pins the FALSE (pre-cutover / rollback) artifact
// behavior regardless of the process-level env the suite runs under.
// The true-artifact router behavior is covered by cutover-modes.spec.js.
vi.mock('../../config/cutover', () => ({
  roomCutoverAuthoritative: false,
  authenticatedLandingRouteName: () => 'Dashboard',
}))

import router from '../index'
import { globalStore } from '../../store'
import { sessionHelper } from '../../services/session'

describe('Router Guard', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.clearUser()
    vi.restoreAllMocks()
  })

  it('rejects a restored user with no session token (clears user, redirects to Auth)', async () => {
    // Simulate: previous tab restored user, but token is missing
    globalStore.setUser({ id: 42, role: 'guest' })
    localStorage.removeItem('lmq_session_token')
    localStorage.removeItem('lmq_session_expires_at')

    await router.push('/')

    expect(globalStore.currentUser).toBeNull()
    expect(router.currentRoute.value.name).toBe('Auth')
  })

  it('rejects a restored user with an expired token', async () => {
    globalStore.setUser({ id: 42, role: 'guest' })
    const past = new Date(Date.now() - 60_000).toISOString()
    sessionHelper.saveSession('expired-token', past)

    await router.push('/')

    expect(globalStore.currentUser).toBeNull()
    expect(router.currentRoute.value.name).toBe('Auth')
    // Stale token must also be evicted from localStorage
    expect(localStorage.getItem('lmq_session_token')).toBeNull()
  })

  it('allows a restored user with a valid persisted token to proceed to Dashboard', async () => {
    globalStore.setUser({ id: 42, role: 'guest' })
    const future = new Date(Date.now() + 60 * 60_000).toISOString()
    sessionHelper.saveSession('valid-token', future)

    await router.push('/')

    expect(globalStore.currentUser).not.toBeNull()
    expect(globalStore.currentUser.id).toBe(42)
    expect(router.currentRoute.value.name).toBe('Dashboard')
  })

  it('redirects an authenticated user away from /auth back to Dashboard', async () => {
    globalStore.setUser({ id: 42, role: 'guest' })
    const future = new Date(Date.now() + 60 * 60_000).toISOString()
    sessionHelper.saveSession('valid-token', future)

    await router.push('/auth')

    expect(router.currentRoute.value.name).toBe('Dashboard')
  })

  it('allows an unauthenticated visitor to reach /auth', async () => {
    await router.push('/auth')

    expect(router.currentRoute.value.name).toBe('Auth')
  })

  it('allows an authenticated user with a valid token to reach /rooms/lobby', async () => {
    globalStore.setUser({ id: 42, role: 'guest' })
    const future = new Date(Date.now() + 60 * 60_000).toISOString()
    sessionHelper.saveSession('valid-token', future)

    await router.push('/rooms/lobby')

    expect(router.currentRoute.value.name).toBe('Room')
    expect(router.currentRoute.value.params.slug).toBe('lobby')
  })

  it('redirects an unauthenticated visitor away from /rooms/lobby to Auth', async () => {
    // Force a navigation so the beforeEach guard re-runs (router.push to the
    // same route after the prior authenticated push is a no-op otherwise).
    await router.push('/auth')
    await router.push('/rooms/lobby')

    expect(router.currentRoute.value.name).toBe('Auth')
  })
})
