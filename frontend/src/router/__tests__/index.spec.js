import { describe, it, expect, beforeEach, vi } from 'vitest'
import router from '../index'
import { globalStore } from '../../store'

describe('Router Guard', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    globalStore.clearUser()
    vi.restoreAllMocks()
  })

  it('a legacy localStorage user without a session is rejected by the route guard', async () => {
    // 1. Set user but no session
    globalStore.setUser({ id: 42, role: 'guest' })
    sessionStorage.clear()

    // 2. Navigate to Dashboard (requires auth)
    await router.push('/')

    // 3. Expect legacy user to be cleared
    expect(globalStore.currentUser).toBeNull()
    
    // 4. Expect to be redirected to Auth
    expect(router.currentRoute.value.name).toBe('Auth')
  })

  it('allows authenticated user with valid session to proceed to Dashboard', async () => {
    globalStore.setUser({ id: 42, role: 'guest' })
    const futureDate = new Date(Date.now() + 1000 * 60 * 60)
    sessionStorage.setItem('lmq_session_token', 'valid-token')
    sessionStorage.setItem('lmq_session_expires_at', futureDate.toISOString())

    await router.push('/')

    expect(globalStore.currentUser).not.toBeNull()
    expect(router.currentRoute.value.name).toBe('Dashboard')
  })
})
