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

  it('prioritizeRoomSong posts ONLY {song_index} — no user_id / requested_by / added_by', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.prioritizeRoomSong('lobby', 2)
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/queue\/prioritize$/)
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body)
    expect(body).toEqual({ song_index: 2 })
    expect(JSON.stringify(body)).not.toMatch(/(user_id|requested_by|added_by|user_role)/)
  })

  // --- R09a playback API ---

  it('setRoomPlaybackStatus posts ONLY {status} and targets /playback/status', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.setRoomPlaybackStatus('lobby', 'paused')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/playback\/status$/)
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body)
    expect(body).toEqual({ status: 'paused' })
    expect(JSON.stringify(body)).not.toMatch(/(user_id|requested_by|added_by|user_role)/)
  })

  it('syncRoomPlayback posts ONLY {elapsed}', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.syncRoomPlayback('lobby', 42)
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/playback\/sync$/)
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body)
    expect(body).toEqual({ elapsed: 42 })
    expect(JSON.stringify(body)).not.toMatch(/(user_id|requested_by|added_by|user_role)/)
  })

  it('skipRoomPlayback posts empty body to /playback/skip', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.skipRoomPlayback('lobby')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/playback\/skip$/)
    expect(init.method).toBe('POST')
    expect(init.body === undefined || init.body === '' || init.body === null).toBe(true)
  })

  it('roomSongEnded posts empty body to /playback/ended', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.roomSongEnded('lobby')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/playback\/ended$/)
    expect(init.method).toBe('POST')
    expect(init.body === undefined || init.body === '' || init.body === null).toBe(true)
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

  // --- R09g room auto-queue API ---

  it('getRoomAutoQueueStatus targets /rooms/{slug}/autoqueue/status with GET and bearer when valid', async () => {
    const future = new Date(Date.now() + 1000 * 60 * 60).toISOString()
    sessionHelper.saveSession('tok', future)
    const mockFetch = mockFetchOk({ enabled: false, strategy: 'related' })
    const res = await api.getRoomAutoQueueStatus('lobby')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/autoqueue\/status$/)
    expect(init.method).toBe('GET')
    expect(init.headers['Authorization']).toBe('Bearer tok')
    expect(res).toEqual({ enabled: false, strategy: 'related' })
  })

  it('setRoomAutoQueueEnabled posts ONLY {enabled} to /rooms/{slug}/autoqueue/toggle', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 200, ok: true, json: async () => ({ enabled: true, strategy: 'related' }) })
    global.fetch = mockFetch
    await api.setRoomAutoQueueEnabled('lobby', true)
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/autoqueue\/toggle$/)
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body)
    expect(body).toEqual({ enabled: true })
    expect(JSON.stringify(body)).not.toMatch(/(user_id|requested_by|added_by|user_role|strategy)/)
  })

  // --- R10c room deletion + member removal API ---

  it('deleteRoom targets DELETE /rooms/{slug} with NO request body', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.deleteRoom('lobby')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby$/)
    expect(init.method).toBe('DELETE')
    // No body shape — must be undefined or empty string.
    expect(init.body === undefined || init.body === '' || init.body === null).toBe(true)
    expect(init.headers['Content-Type']).toBe('application/json')
  })

  it('removeRoomMember targets DELETE /rooms/{slug}/members/{userId} with NO request body', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    await api.removeRoomMember('lobby', 42)
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/members\/42$/)
    expect(init.method).toBe('DELETE')
    expect(init.body === undefined || init.body === '' || init.body === null).toBe(true)
    expect(init.headers['Content-Type']).toBe('application/json')
  })

  it('deleteRoom returns null on 204 (no JSON body to parse)', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    const res = await api.deleteRoom('lobby')
    expect(res).toBeNull()
  })

  it('removeRoomMember returns null on 204', async () => {
    const mockFetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: async () => '' })
    global.fetch = mockFetch
    const res = await api.removeRoomMember('lobby', 7)
    expect(res).toBeNull()
  })

  it('deleteRoom propagates APIError on 400 / 401 / 403 / 404 / 409', async () => {
    for (const status of [400, 401, 403, 404, 409]) {
      const mockFetch = vi.fn().mockResolvedValue({ status, ok: false, text: async () => `err ${status}` })
      global.fetch = mockFetch
      await expect(api.deleteRoom('lobby')).rejects.toMatchObject({ status })
    }
  })

  it('removeRoomMember propagates APIError on 400 / 401 / 403 / 404 / 409', async () => {
    for (const status of [400, 401, 403, 404, 409]) {
      const mockFetch = vi.fn().mockResolvedValue({ status, ok: false, text: async () => `err ${status}` })
      global.fetch = mockFetch
      await expect(api.removeRoomMember('lobby', 7)).rejects.toMatchObject({ status })
    }
  })

  // --- R10c member-list read surface ---

  it('getRoomMembers targets GET /rooms/{slug}/members with bearer when valid', async () => {
    const future = new Date(Date.now() + 1000 * 60 * 60).toISOString()
    sessionHelper.saveSession('tok', future)
    const mockFetch = mockFetchOk({ members: [{ user_id: 1, role: 'host' }] })
    const res = await api.getRoomMembers('lobby')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/members$/)
    expect(init.method).toBe('GET')
    expect(init.body === undefined || init.body === '' || init.body === null).toBe(true)
    expect(init.headers['Authorization']).toBe('Bearer tok')
    expect(res).toEqual({ members: [{ user_id: 1, role: 'host' }] })
  })

  it('getRoomMembers propagates APIError on 401 / 403 / 404 / 409', async () => {
    for (const status of [401, 403, 404, 409]) {
      const mockFetch = vi.fn().mockResolvedValue({ status, ok: false, text: async () => `err ${status}` })
      global.fetch = mockFetch
      await expect(api.getRoomMembers('lobby')).rejects.toMatchObject({ status })
    }
  })
})
// --- R11a room chat (frontend API) ---

describe('Room Chat API (R11a)', () => {
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

  it('getRoomChatMessages targets GET /rooms/{slug}/chat/messages with default limit', async () => {
    const future = new Date(Date.now() + 1000 * 60 * 60).toISOString()
    sessionHelper.saveSession('tok', future)
    const mockFetch = mockFetchOk({ messages: [] })
    const res = await api.getRoomChatMessages('lobby')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/chat\/messages(\?.*)?$/)
    expect(init.method).toBe('GET')
    expect(init.headers['Authorization']).toBe('Bearer tok')
    expect(res).toEqual({ messages: [] })
  })

  it('getRoomChatMessages appends ?limit=N when a positive numeric limit is supplied', async () => {
    const future = new Date(Date.now() + 1000 * 60 * 60).toISOString()
    sessionHelper.saveSession('tok', future)
    const mockFetch = mockFetchOk({ messages: [] })
    await api.getRoomChatMessages('lobby', 25)
    const [url] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/chat\/messages\?limit=25$/)
  })

  it('getRoomChatMessages omits ?limit= when limit is 0 or NaN', async () => {
    const future = new Date(Date.now() + 1000 * 60 * 60).toISOString()
    sessionHelper.saveSession('tok', future)
    for (const lim of [0, NaN]) {
      const mockFetch = mockFetchOk({ messages: [] })
      await api.getRoomChatMessages('lobby', lim)
      const [url] = mockFetch.mock.calls[0]
      expect(url).not.toMatch(/limit=/)
    }
  })

  it('getRoomChatMessages URL-encodes the slug', async () => {
    const future = new Date(Date.now() + 1000 * 60 * 60).toISOString()
    sessionHelper.saveSession('tok', future)
    const mockFetch = mockFetchOk({ messages: [] })
    await api.getRoomChatMessages('weird slug')
    const [url] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/weird%20slug\/chat\/messages(\?.*)?$/)
  })

  it('sendRoomChatMessage posts body { content } only — no sender_id, no user_id', async () => {
    const mockFetch = mockFetchOk({
      id: 7, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' },
      content: 'hi', created_at: new Date().toISOString(),
    })
    await api.sendRoomChatMessage('lobby', 'hi')
    const [url, init] = mockFetch.mock.calls[0]
    expect(url).toMatch(/\/api\/rooms\/lobby\/chat\/messages$/)
    expect(init.method).toBe('POST')
    const body = JSON.parse(init.body)
    expect(body).toEqual({ content: 'hi' })
    expect(body).not.toHaveProperty('sender_id')
    expect(body).not.toHaveProperty('user_id')
  })

  it('sendRoomChatMessage stringifies non-string content safely', async () => {
    const mockFetch = mockFetchOk({ id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: '', created_at: new Date().toISOString() })
    await api.sendRoomChatMessage('lobby', null)
    const [, init] = mockFetch.mock.calls[0]
    const body = JSON.parse(init.body)
    expect(body).toEqual({ content: '' })
  })

  it('sendRoomChatMessage propagates APIError on 400 / 401 / 403 / 404 / 409', async () => {
    for (const status of [400, 401, 403, 404, 409]) {
      const mockFetch = vi.fn().mockResolvedValue({ status, ok: false, text: async () => `err ${status}` })
      global.fetch = mockFetch
      await expect(api.sendRoomChatMessage('lobby', 'hi')).rejects.toMatchObject({ status })
    }
  })

  it('sendRoomChatMessage returns { message: ... } (post-mutation envelope is wrapped)', async () => {
    const future = new Date(Date.now() + 1000 * 60 * 60).toISOString()
    sessionHelper.saveSession('tok', future)
    const mockFetch = mockFetchOk({
      message: { id: 7, room_slug: 'lobby', sender: { user_id: 1, display_name: 'Me' }, content: 'hi', created_at: '2026-01-01T00:00:00Z' },
    })
    const res = await api.sendRoomChatMessage('lobby', 'hi')
    expect(res).toBeDefined()
    expect(res.message).toBeDefined()
    expect(res.message.id).toBe(7)
    expect(res.message.content).toBe('hi')
    // No email in the response.
    expect(JSON.stringify(res)).not.toMatch(/email/)
  })
})
