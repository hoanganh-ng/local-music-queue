import { describe, it, expect, beforeEach, vi } from 'vitest'
import { globalStore } from '../../store'
import { sessionHelper } from '../../services/session'

// R14d cutover-modes: TRUE (post-cutover) artifact router behavior.
//
// This file pins the production router with the cutover gate mocked to
// TRUE, regardless of the process-level env the suite runs under. The
// false-artifact behavior is pinned by router.spec.js / index.spec.js.
//
// Contract under test:
//   - no route named Dashboard exists (DashboardView cannot mount
//     through normal routing);
//   - `/` redirects to the named RoomEntry route;
//   - authenticated /auth resolves to RoomEntry;
//   - /rooms and /rooms/:slug remain unchanged.

vi.mock('../../views/DashboardView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../../views/AuthView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../../views/RoomView.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../../views/RoomEntryView.vue', () => ({ default: { template: '<div />' } }))

vi.mock('../../config/cutover', () => ({
  parseRoomCutoverAuthoritative: (raw) => raw === 'true',
  roomCutoverAuthoritative: true,
  authenticatedLandingRouteName: () => 'RoomEntry',
}))

async function buildProductionRouter() {
  const mod = await import('../index')
  return mod.default
}

function authenticate() {
  const future = new Date(Date.now() + 60 * 60_000).toISOString()
  sessionHelper.saveSession('opaque-token', future)
  globalStore.setUser({ id: 1, display_name: 'Me', role: 'host' })
}

describe('Production router — true (post-cutover) artifact (R14d)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.clearUser()
    vi.clearAllMocks()
  })

  it('no route named Dashboard exists', async () => {
    const router = await buildProductionRouter()
    expect(router.hasRoute('Dashboard')).toBe(false)
  })

  it('the / record is a pure redirect — no component can mount at /', async () => {
    const router = await buildProductionRouter()
    const rootRecord = router.getRoutes().find((r) => r.path === '/')
    expect(rootRecord).toBeDefined()
    expect(rootRecord.redirect).toEqual({ name: 'RoomEntry' })
    expect(rootRecord.components).toBeFalsy()
  })

  it('authenticated / lands on RoomEntry', async () => {
    authenticate()
    const router = await buildProductionRouter()
    await router.replace('/auth').catch(() => {})
    await router.push('/')
    expect(router.currentRoute.value.name).toBe('RoomEntry')
    expect(router.currentRoute.value.path).toBe('/rooms')
  })

  it('authenticated /auth resolves to RoomEntry (shared landing decision)', async () => {
    authenticate()
    const router = await buildProductionRouter()
    await router.replace('/rooms').catch(() => {})
    await router.push('/auth')
    expect(router.currentRoute.value.name).toBe('RoomEntry')
  })

  it('unauthenticated / still redirects to Auth (RoomEntry requires auth)', async () => {
    const router = await buildProductionRouter()
    await router.replace('/auth').catch(() => {})
    await router.push('/')
    expect(router.currentRoute.value.name).toBe('Auth')
  })

  it('/rooms and /rooms/:slug remain unchanged authenticated room routes', async () => {
    const router = await buildProductionRouter()
    const rooms = router.resolve('/rooms')
    expect(rooms.name).toBe('RoomEntry')
    expect(rooms.meta.requiresAuth).toBe(true)
    const room = router.resolve('/rooms/lobby')
    expect(room.name).toBe('Room')
    expect(room.meta.requiresAuth).toBe(true)
    expect(room.params.slug).toBe('lobby')
  })

  it('authenticated direct /rooms/:slug navigation works', async () => {
    authenticate()
    const router = await buildProductionRouter()
    await router.push('/rooms/lobby')
    expect(router.currentRoute.value.name).toBe('Room')
    expect(router.currentRoute.value.params.slug).toBe('lobby')
  })
})
