import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AuthView from '../AuthView.vue'
import { globalStore } from '../../store'
import { api } from '../../services/api'
import { sessionHelper } from '../../services/session'

vi.mock('../../services/api', () => ({
  api: {
    loginWithGoogle: vi.fn(),
  }
}))

// R14d: pin this suite to the pre-cutover (false) artifact so it stays
// deterministic no matter which VITE_ROOM_CUTOVER_AUTHORITATIVE value the
// whole test run is baked with. The true-artifact login landing
// (RoomEntry) is covered by __tests__/cutover-modes.spec.js.
vi.mock('../../config/cutover', () => ({
  parseRoomCutoverAuthoritative: (raw) => raw === 'true',
  roomCutoverAuthoritative: false,
  authenticatedLandingRouteName: () => 'Dashboard',
}))

const pushMock = vi.hoisted(() => vi.fn())
vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: pushMock,
  })
}))

describe('AuthView', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    globalStore.clearUser()
    vi.restoreAllMocks()
  })

  it('Google login extracts session_token and session_expires_at, and does not store them in persisted user document', async () => {
    const mockUserData = {
      id: 42,
      email: 'user@urekamedia.vn',
      display_name: 'Test User',
      role: 'guest',
      session_token: 'extracted-session-token',
      session_expires_at: '2026-06-19T23:59:59Z',
    }
    
    api.loginWithGoogle.mockResolvedValueOnce(mockUserData)

    const wrapper = mount(AuthView)
    
    // AuthView sets window.handleGoogleCallback on mounted
    expect(window.handleGoogleCallback).toBeTypeOf('function')

    // Simulate Google callback
    await window.handleGoogleCallback({ credential: 'google-oauth-credential-token' })

    // Verify localStorage stores session info (persistent across tab reopen)
    expect(localStorage.getItem('lmq_session_token')).toBe('extracted-session-token')
    expect(localStorage.getItem('lmq_session_expires_at')).toBe('2026-06-19T23:59:59Z')
    // sessionStorage must not be used for tokens
    expect(sessionStorage.getItem('lmq_session_token')).toBeNull()

    // Verify globalStore does not have session fields
    expect(globalStore.currentUser.session_token).toBeUndefined()
    expect(globalStore.currentUser.session_expires_at).toBeUndefined()
    expect(globalStore.currentUser.id).toBe(42)

    // Verify localStorage (persisted currentUser) does not contain session fields
    const storedUser = JSON.parse(localStorage.getItem('lmq_user_session'))
    expect(storedUser).not.toBeNull()
    expect(storedUser.session_token).toBeUndefined()
    expect(storedUser.session_expires_at).toBeUndefined()
    expect(storedUser.id).toBe(42)

    // R14d (false artifact): successful login navigates to the shared
    // authenticated landing decision — the Dashboard.
    expect(pushMock).toHaveBeenCalledWith({ name: 'Dashboard' })
  })
})
