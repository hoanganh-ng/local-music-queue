import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { globalStore } from '../../store'
import { sessionHelper } from '../../services/session'

// R05b1 production router tests.
//
// These tests pin the production `frontend/src/router/index.js`
// against the documented guard contract:
//   - unauthenticated /rooms  → Auth
//   - authenticated   /rooms  → RoomEntry
//   - authenticated   /auth  → Dashboard
//
// The view-level tests use a synthetic router; this file
// exercises the actual production router module so the production
// route registration + beforeEach guard are not silently regressed.

vi.mock('../../views/DashboardView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../../views/AuthView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../../views/RoomView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../../views/RoomEntryView.vue', () => ({ default: { template: '<div />' } }))

// R14d: this file pins the FALSE (pre-cutover / rollback) artifact
// behavior regardless of the process-level env the suite runs under.
// The true-artifact router behavior is covered by cutover-modes.spec.js.
vi.mock('../../config/cutover', () => ({
  roomCutoverAuthoritative: false,
  authenticatedLandingRouteName: () => 'Dashboard',
}))

// Stub the lazy import so the dynamic imports in the router
// resolve synchronously under jsdom.
vi.mock('../../views/__tests__/stub-empty', () => ({ default: { template: '<div />' } }))

function loadProductionRouter() {
  // The production router is a module-level singleton (constructed
  // once at first import). Tests share that cached instance; each
  // case resets auth/session state in `beforeEach` and performs
  // an explicit navigation so the guard runs against a clean slate.
  return import('../index')
}

async function buildProductionRouter() {
  const mod = await loadProductionRouter()
  return mod.default
}

describe('Production router (R05b1)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.clearUser()
    vi.clearAllMocks()
  })

  it('exposes the /rooms route as name=RoomEntry with requiresAuth=true', async () => {
    const router = await buildProductionRouter()
    // Replace the production web history with a memory history so
    // the test does not depend on document.location.
    router.history = createMemoryHistory()
    const route = router.resolve('/rooms')
    expect(route.name).toBe('RoomEntry')
    expect(route.meta.requiresAuth).toBe(true)
  })

  it('unauthenticated /rooms redirects to Auth', async () => {
    const router = await buildProductionRouter()
    await router.replace('/auth').catch(() => {})
    await router.push('/rooms')
    expect(router.currentRoute.value.name).toBe('Auth')
  })

  it('authenticated /rooms lands on RoomEntry', async () => {
    const future = new Date(Date.now() + 60 * 60_000).toISOString()
    sessionHelper.saveSession('opaque-token', future)
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })
    const router = await buildProductionRouter()
    await router.push('/rooms')
    expect(router.currentRoute.value.name).toBe('RoomEntry')
    expect(router.currentRoute.value.path).toBe('/rooms')
  })

  it('authenticated /auth redirects to Dashboard (R14d owns changing the default)', async () => {
    const future = new Date(Date.now() + 60 * 60_000).toISOString()
    sessionHelper.saveSession('opaque-token', future)
    globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })
    const router = await buildProductionRouter()
    await router.push('/auth')
    expect(router.currentRoute.value.name).toBe('Dashboard')
  })

  it('unauthenticated /rooms/:slug redirects to Auth', async () => {
    const router = await buildProductionRouter()
    await router.push('/rooms/lobby')
    expect(router.currentRoute.value.name).toBe('Auth')
  })

  it('unauthenticated / redirects to Auth', async () => {
    const router = await buildProductionRouter()
    await router.push('/')
    expect(router.currentRoute.value.name).toBe('Auth')
  })

  it('stale localStorage user without a valid session is cleared and the visit is redirected to Auth', async () => {
    // No session but a leftover user — the guard should clear the
    // stale user and redirect to Auth (not silently land on a
    // protected route with stale identity).
    globalStore.setUser({ id: 1, display_name: 'Stale', role: 'host' })
    const router = await buildProductionRouter()
    await router.push('/rooms')
    expect(router.currentRoute.value.name).toBe('Auth')
    expect(globalStore.currentUser).toBeNull()
    expect(sessionHelper.getToken()).toBeNull()
  })
})