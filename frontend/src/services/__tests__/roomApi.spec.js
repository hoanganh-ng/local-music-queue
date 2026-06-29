import { describe, it, expect, beforeEach, vi } from 'vitest'
import { api } from '../api'
import { sessionHelper } from '../session'

describe('Room Queue API', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    sessionHelper.clearSession()
    vi.restoreAllMocks()
  })

  function mockFetchOk(json = {}) {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 200, ok: true, json: async () => json
    })
    global.fetch = mockFetch
    return mockFetch
  }

  it('getRoomQueue targets /rooms/{slug}/queue with GET and bearer when valid', async () => {
    const future = new Date(Date.now() + 1000 * 60 * 60).toISOString()
    sessionHelper.saveSession('tok', future)
    const mockFetch = mockFetchOk({ songs: [], current_index: -1 })
    await api.getRoomQueue('lobby')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/queue$/)
    expect(init.method).toBe('GET')
    expect(init.headers['Authorization']).toBe('Bearer tok')
  })

  it('addRoomSong posts ONLY {url, metadata} — no added_by / added_by_id fields', async () => {
    const mockFetch = mockFetchOk({ id: 's1' })
    await api.addRoomSong('lobby', 'https://youtu.be/abc', { title: 'T' })
    const [, init] = mockFetch.mock.calls[0]
    const body = JSON.parse(init.body)
    expect(body).toEqual({ url: 'https://youtu.be/abc', metadata: { title: 'T' } })
    expect(body).not.toHaveProperty('added_by')
    expect(body).not.toHaveProperty('added_by_id')
  })

  it('addRoomSong omits metadata when null', async () => {
    const mockFetch = mockFetchOk({ id: 's1' })
    await api.addRoomSong('lobby', 'https://youtu.be/abc')
    const [, init] = mockFetch.mock.calls[0]
    const body = JSON.parse(init.body)
    expect(body).toEqual({ url: 'https://youtu.be/abc' })
  })

  it('removeRoomSong posts ONLY {index}', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.removeRoomSong('lobby', 2)
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/queue\/remove$/)
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ index: 2 })
    expect(JSON.stringify(JSON.parse(init.body))).not.toMatch(/(added_by|requested_by|user_id|user_role)/)
  })

  it('clearRoomQueue posts empty body and ignores client identity fields', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.clearRoomQueue('lobby')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/queue\/clear$/)
    expect(init.method).toBe('POST')
    // Empty body for 204-clean shape; either undefined or '{}' is acceptable.
    const bodyText = init.body ?? '{}'
    expect(JSON.parse(bodyText)).toEqual({})
    expect(bodyText).not.toMatch(/(added_by|requested_by|user_id|user_role)/)
  })

  it('omits Authorization when session invalid', async () => {
    const mockFetch = mockFetchOk({ songs: [] })
    await api.getRoomQueue('lobby')
    const [, init] = mockFetch.mock.calls[0]
    expect(init.headers['Authorization']).toBeUndefined()
  })

  it('propagates APIError with backend status on non-ok (401, 403, 404, 409)', async () => {
    for (const status of [401, 403, 404, 409]) {
      const mockFetch = vi.fn().mockResolvedValue({ status, ok: false, text: async () => `err ${status}` })
      global.fetch = mockFetch
      await expect(api.getRoomQueue('lobby')).rejects.toMatchObject({ status })
    }
  })
})