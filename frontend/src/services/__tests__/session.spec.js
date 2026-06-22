import { describe, it, expect, beforeEach } from 'vitest'
import { sessionHelper } from '../session'

describe('sessionHelper', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
  })

  it('saveSession persists token and expiry to localStorage', () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession('tok-123', future)

    expect(localStorage.getItem('lmq_session_token')).toBe('tok-123')
    expect(localStorage.getItem('lmq_session_expires_at')).toBe(future)

    // Must NOT leak into sessionStorage
    expect(sessionStorage.getItem('lmq_session_token')).toBeNull()
    expect(sessionStorage.getItem('lmq_session_expires_at')).toBeNull()
  })

  it('getToken and getExpiresAt return values previously saved', () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession('tok-xyz', future)

    expect(sessionHelper.getToken()).toBe('tok-xyz')
    expect(sessionHelper.getExpiresAt()).toBe(future)
  })

  it('clearSession removes token and expiry from localStorage', () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession('tok-abc', future)

    sessionHelper.clearSession()

    expect(localStorage.getItem('lmq_session_token')).toBeNull()
    expect(localStorage.getItem('lmq_session_expires_at')).toBeNull()
  })

  it('isValid returns true when token exists and expiry is in the future', () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    sessionHelper.saveSession('tok-future', future)
    expect(sessionHelper.isValid()).toBe(true)
  })

  it('isValid returns false when expiry is in the past', () => {
    const past = new Date(Date.now() - 60_000).toISOString()
    sessionHelper.saveSession('tok-past', past)
    expect(sessionHelper.isValid()).toBe(false)
  })

  it('isValid returns false when token is missing', () => {
    localStorage.setItem('lmq_session_expires_at', new Date(Date.now() + 60_000).toISOString())
    expect(sessionHelper.isValid()).toBe(false)
  })

  it('isValid returns false when expiry is missing', () => {
    localStorage.setItem('lmq_session_token', 'tok-noexpiry')
    expect(sessionHelper.isValid()).toBe(false)
  })
})
