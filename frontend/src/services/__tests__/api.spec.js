import { describe, it, expect, beforeEach, vi } from 'vitest'
import { api, APIError } from '../api'
import { sessionHelper } from '../session'

describe('API Service', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    vi.restoreAllMocks()
  })

  it('attaches bearer token when valid session exists', async () => {
    const futureDate = new Date(Date.now() + 1000 * 60 * 60)
    sessionHelper.saveSession('valid-token', futureDate.toISOString())

    const mockFetch = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      json: async () => ({ success: true })
    })
    global.fetch = mockFetch

    await api.request('/test')

    expect(mockFetch).toHaveBeenCalled()
    const callArgs = mockFetch.mock.calls[0]
    expect(callArgs[1].headers['Authorization']).toBe('Bearer valid-token')
  })

  it('does not attach token when session is expired', async () => {
    const pastDate = new Date(Date.now() - 1000 * 60 * 60)
    sessionHelper.saveSession('expired-token', pastDate.toISOString())

    const mockFetch = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      json: async () => ({ success: true })
    })
    global.fetch = mockFetch

    await api.request('/test')

    expect(mockFetch).toHaveBeenCalled()
    const callArgs = mockFetch.mock.calls[0]
    expect(callArgs[1].headers['Authorization']).toBeUndefined()
  })

  it('throws APIError with status code when response is not ok', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 403,
      ok: false,
      text: async () => 'Forbidden request'
    })
    global.fetch = mockFetch

    await expect(api.request('/test')).rejects.toThrow(APIError)
    
    try {
      await api.request('/test')
    } catch (err) {
      expect(err.status).toBe(403)
      expect(err.message).toBe('Forbidden request')
    }
  })
})
