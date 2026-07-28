import { describe, it, expect, beforeEach, vi } from 'vitest'
import { shallowMount, mount, flushPromises } from '@vue/test-utils'
import { globalStore } from '../../store'
import { sessionHelper } from '../../services/session'

// R14d cutover-modes: TRUE (post-cutover) artifact view behavior.
//
// This file pins RoomEntryView and AuthView with the cutover gate
// mocked to TRUE, regardless of the process-level env the suite runs
// under. The false-artifact behavior (Back to dashboard, login →
// Dashboard) is pinned by RoomEntryView.spec.js and AuthView.spec.js.

const apiMock = vi.hoisted(() => ({
  listRooms: vi.fn(),
  getRoom: vi.fn(),
  createRoom: vi.fn(),
  redeemInvite: vi.fn(),
  loginWithGoogle: vi.fn(),
}))
vi.mock('../../services/api', () => ({ api: apiMock }))

const toastMock = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn(), info: vi.fn() }))
vi.mock('../../composables/useToast', () => ({ useToast: () => toastMock }))

vi.mock('../../config/cutover', () => ({
  parseRoomCutoverAuthoritative: (raw) => raw === 'true',
  roomCutoverAuthoritative: true,
  authenticatedLandingRouteName: () => 'RoomEntry',
}))

const pushMock = vi.hoisted(() => vi.fn())
vi.mock('vue-router', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    useRouter: () => ({ push: pushMock }),
  }
})

import RoomEntryView from '../RoomEntryView.vue'
import AuthView from '../AuthView.vue'

describe('RoomEntryView — true (post-cutover) artifact (R14d)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.clearUser()
    vi.resetAllMocks()
    apiMock.listRooms.mockResolvedValue([])
  })

  it('hides the dashboard control', async () => {
    const wrapper = shallowMount(RoomEntryView)
    await flushPromises()
    expect(wrapper.find('[data-testid="dashboard-btn"]').exists()).toBe(false)
  })

  it('shows a visible sign-out control', async () => {
    const wrapper = shallowMount(RoomEntryView)
    await flushPromises()
    const btn = wrapper.find('[data-testid="sign-out-btn"]')
    expect(btn.exists()).toBe(true)
    expect(btn.text()).toMatch(/sign out/i)
  })

  it('sign-out clears the session and current user, then navigates to Auth — without exposing the token', async () => {
    const token = 'opaque-signout-token'
    const future = new Date(Date.now() + 60 * 60_000).toISOString()
    sessionHelper.saveSession(token, future)
    globalStore.setUser({ id: 9, display_name: 'Me', role: 'host' })

    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {})
    const infoSpy = vi.spyOn(console, 'info').mockImplementation(() => {})
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    const wrapper = shallowMount(RoomEntryView)
    await flushPromises()
    await wrapper.find('[data-testid="sign-out-btn"]').trigger('click')

    expect(sessionHelper.getToken()).toBeNull()
    expect(localStorage.getItem('lmq_session_token')).toBeNull()
    expect(globalStore.currentUser).toBeNull()
    expect(pushMock).toHaveBeenCalledWith({ name: 'Auth' })

    for (const spy of [logSpy, infoSpy, warnSpy, errorSpy]) {
      for (const call of spy.mock.calls) {
        expect(JSON.stringify(call)).not.toContain(token)
      }
      spy.mockRestore()
    }
  })
})

describe('AuthView — true (post-cutover) artifact (R14d)', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.clearUser()
    vi.resetAllMocks()
  })

  it('successful Google login navigates to RoomEntry (shared landing decision)', async () => {
    apiMock.loginWithGoogle.mockResolvedValueOnce({
      id: 42,
      email: 'user@example.com',
      display_name: 'Test User',
      role: 'guest',
      session_token: 'extracted-session-token',
      session_expires_at: '2027-06-19T23:59:59Z',
    })

    mount(AuthView)
    expect(window.handleGoogleCallback).toBeTypeOf('function')
    await window.handleGoogleCallback({ credential: 'google-oauth-credential-token' })

    expect(pushMock).toHaveBeenCalledWith({ name: 'RoomEntry' })
  })
})
