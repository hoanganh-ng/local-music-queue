import { describe, it, expect, beforeEach } from 'vitest'
import { globalStore } from '../index'

describe('Room queue store (isolated)', () => {
  beforeEach(() => {
    // Snapshot global state and reset between tests.
    globalStore.roomQueues = {}
    // Make sure global queue state is at a known baseline so we can assert
    // room events NEVER mutate it.
    globalStore.queueState = { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] }
    globalStore.voteSessions = {}
    globalStore.autoQueueConfig = { enabled: false, strategy: 'related' }
  })

  it('setRoomQueueConnected flips the connected flag for a slug', () => {
    globalStore.setRoomQueueConnected('lobby', true)
    expect(globalStore.roomQueues.lobby.connected).toBe(true)
  })

  it('setRoomQueueState replaces state and clears lastError', () => {
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.setRoomQueueError('lobby', 'boom')
    const newState = { songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] }
    globalStore.setRoomQueueState('lobby', newState)
    expect(globalStore.roomQueues.lobby.state).toMatchObject(newState)
    expect(globalStore.roomQueues.lobby.lastError).toBeNull()
  })

  it('applyRoomSongAdded prefers fullState when provided', () => {
    const postState = { songs: [{ id: 'a' }, { id: 'b' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [{ id: 'b' }], history: [] }
    globalStore.applyRoomSongAdded('lobby', { id: 'b' }, 1, postState)
    expect(globalStore.roomQueues.lobby.state).toMatchObject(postState)
  })

  it('applyRoomSongRemoved uses fullState when provided, else splices and decrements current_index', () => {
    // fullState path
    const post = { songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] }
    globalStore.setRoomQueueState('lobby', { songs: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] })
    globalStore.applyRoomSongRemoved('lobby', 1, post)
    expect(globalStore.roomQueues.lobby.state).toMatchObject(post)

    // Fallback path
    globalStore.setRoomQueueState('room2', { songs: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] })
    globalStore.applyRoomSongRemoved('room2', 2) // no fullState
    const s = globalStore.roomQueues.room2.state
    expect(s.songs.map(x => x.id)).toEqual(['a', 'b'])
    expect(s.current_index).toBe(0)
  })

  it('applyRoomQueueCleared uses fullState when provided, else resets', () => {
    const post = { songs: [{ id: 'cur' }], current_index: 0, current_song: { id: 'cur' }, status: 'paused', queue: [], history: [] }
    globalStore.setRoomQueueState('lobby', { songs: [{ id: 'a' }, { id: 'b' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] })
    globalStore.applyRoomQueueCleared('lobby', post)
    expect(globalStore.roomQueues.lobby.state).toMatchObject(post)

    globalStore.setRoomQueueState('room2', { songs: [{ id: 'a' }, { id: 'b' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] })
    globalStore.applyRoomQueueCleared('room2')
    expect(globalStore.roomQueues.room2.state.songs).toEqual([])
    expect(globalStore.roomQueues.room2.state.current_index).toBe(-1)
    expect(globalStore.roomQueues.room2.state.current_song).toBeNull()
  })

  it('clearRoomQueueState deletes the slug entry', () => {
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.clearRoomQueueState('lobby')
    expect(globalStore.roomQueues.lobby).toBeUndefined()
  })

  it('applyRoomSongPrioritized prefers fullState when provided', () => {
    const post = { songs: [{ id: 'cur' }, { id: 'moved', is_prioritized: true }], current_index: 0, current_song: { id: 'cur' }, status: 'playing', queue: [], history: [] }
    globalStore.setRoomQueueState('lobby', { songs: [{ id: 'cur' }, { id: 'moved' }, { id: 'other' }], current_index: 0, current_song: { id: 'cur' }, status: 'playing', queue: [], history: [] })
    globalStore.applyRoomSongPrioritized('lobby', 2, 1, { id: 'moved' }, post)
    expect(globalStore.roomQueues.lobby.state).toMatchObject(post)
  })

  it('applyRoomSongPrioritized fallback: moves song, stamps is_prioritized, compensates current_index', () => {
    // current at 0; upcoming at 1 and 2. Move idx 2 to idx 1.
    globalStore.setRoomQueueState('lobby', {
      songs: [{ id: 'cur' }, { id: 'a' }, { id: 'b' }],
      current_index: 0,
      current_song: { id: 'cur' },
      status: 'playing',
      queue: [],
      history: [],
    })
    globalStore.applyRoomSongPrioritized('lobby', 2, 1, { id: 'b' }, null)
    const s = globalStore.roomQueues.lobby.state
    expect(s.songs.map(x => x.id)).toEqual(['cur', 'b', 'a'])
    // moved song carries is_prioritized=true
    expect(s.songs[1].is_prioritized).toBe(true)
    // current_index unchanged (from 2 to 1 is after current 0)
    expect(s.current_index).toBe(0)
  })

  it('room mutators NEVER touch global queueState / voteSessions / autoQueueConfig / currentUser', () => {
    const before = {
      queueState: JSON.parse(JSON.stringify(globalStore.queueState)),
      voteSessions: { ...globalStore.voteSessions },
      autoQueueConfig: { ...globalStore.autoQueueConfig },
      currentUser: globalStore.currentUser,
    }
    globalStore.setRoomQueueConnected('lobby', true)
    globalStore.setRoomQueueState('lobby', { songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', queue: [], history: [] })
    globalStore.applyRoomSongAdded('lobby', { id: 'b' }, 1, null)
    globalStore.applyRoomSongRemoved('lobby', 0, null)
    globalStore.applyRoomQueueCleared('lobby')
    globalStore.applyRoomPlaybackStatusChanged('lobby', { status: 'paused', elapsed: 5, state: null })
    globalStore.applyRoomPlaybackElapsedSync('lobby', 17)
    globalStore.applyRoomPlaybackSongAdvanced('lobby', { reason: 'skip', previous_index: 0, new_index: 1, current_song: { id: 'b' }, status: 'playing', elapsed: 0, state: null })
    expect(globalStore.queueState).toEqual(before.queueState)
    expect(globalStore.voteSessions).toEqual(before.voteSessions)
    expect(globalStore.autoQueueConfig).toEqual(before.autoQueueConfig)
    expect(globalStore.currentUser).toBe(before.currentUser)
  })

  // --- R09a playback mutators ---

  it('applyRoomPlaybackStatusChanged prefers fullState when present', () => {
    const fullState = { songs: [], current_index: 0, current_song: { id: 'x' }, status: 'paused', elapsed: 9, queue: [], history: [] }
    globalStore.applyRoomPlaybackStatusChanged('lobby', { status: 'paused', elapsed: 9, state: fullState })
    expect(globalStore.roomQueues.lobby.state).toEqual(fullState)
  })

  it('applyRoomPlaybackStatusChanged fallback: stamps status + elapsed', () => {
    globalStore.setRoomQueueState('lobby', { songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', elapsed: 0, queue: [], history: [] })
    globalStore.applyRoomPlaybackStatusChanged('lobby', { status: 'paused', elapsed: 7 })
    expect(globalStore.roomQueues.lobby.state.status).toBe('paused')
    expect(globalStore.roomQueues.lobby.state.elapsed).toBe(7)
  })

  it('applyRoomPlaybackElapsedSync prefers fullState when present', () => {
    const fullState = { songs: [], current_index: 0, current_song: { id: 'x' }, status: 'playing', elapsed: 33, queue: [], history: [] }
    globalStore.applyRoomPlaybackElapsedSync('lobby', { elapsed: 33, state: fullState })
    expect(globalStore.roomQueues.lobby.state).toEqual(fullState)
  })

  it('applyRoomPlaybackElapsedSync fallback: stamps elapsed (accepts number or {elapsed} payload)', () => {
    globalStore.setRoomQueueState('lobby', { songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', elapsed: 0, queue: [], history: [] })
    globalStore.applyRoomPlaybackElapsedSync('lobby', 12)
    expect(globalStore.roomQueues.lobby.state.elapsed).toBe(12)
    globalStore.applyRoomPlaybackElapsedSync('lobby', { elapsed: 21 })
    expect(globalStore.roomQueues.lobby.state.elapsed).toBe(21)
  })

  it('applyRoomPlaybackSongAdvanced prefers fullState when present', () => {
    const fullState = { songs: [{ id: 'b' }], current_index: 0, current_song: { id: 'b' }, status: 'playing', elapsed: 0, queue: [], history: [] }
    globalStore.applyRoomPlaybackSongAdvanced('lobby', { reason: 'skip', previous_index: 0, new_index: 0, current_song: { id: 'b' }, status: 'playing', elapsed: 0, state: fullState })
    expect(globalStore.roomQueues.lobby.state).toEqual(fullState)
  })

  it('applyRoomPlaybackSongAdvanced fallback: stamps current_index / current_song / status / elapsed', () => {
    globalStore.setRoomQueueState('lobby', { songs: [{ id: 'a' }, { id: 'b' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', elapsed: 99, queue: [], history: [] })
    globalStore.applyRoomPlaybackSongAdvanced('lobby', { reason: 'skip', previous_index: 0, new_index: 1, current_song: { id: 'b' }, status: 'playing', elapsed: 0 })
    expect(globalStore.roomQueues.lobby.state.current_index).toBe(1)
    expect(globalStore.roomQueues.lobby.state.current_song).toEqual({ id: 'b' })
    expect(globalStore.roomQueues.lobby.state.status).toBe('playing')
    expect(globalStore.roomQueues.lobby.state.elapsed).toBe(0)
  })

  // --- R09g room auto-queue mutators ---

  it('_ensureRoomEntry seeds autoQueueConfig to {enabled:false, strategy:"related"}', () => {
    globalStore.setRoomQueueConnected('lobby', true)
    expect(globalStore.roomQueues.lobby.autoQueueConfig).toEqual({ enabled: false, strategy: 'related' })
  })

  it('setRoomAutoQueueConfig replaces per-room autoQueueConfig and never touches global autoQueueConfig', () => {
    const before = { ...globalStore.autoQueueConfig }
    globalStore.setRoomAutoQueueConfig('lobby', true, 'related')
    expect(globalStore.roomQueues.lobby.autoQueueConfig).toEqual({ enabled: true, strategy: 'related' })
    expect(globalStore.autoQueueConfig).toEqual(before)
  })

  it('applyRoomAutoQueueConfigChanged replaces per-room autoQueueConfig from payload only', () => {
    globalStore.applyRoomAutoQueueConfigChanged('lobby', { room_slug: 'lobby', enabled: true, strategy: 'related' })
    expect(globalStore.roomQueues.lobby.autoQueueConfig).toEqual({ enabled: true, strategy: 'related' })
    // global autoQueueConfig MUST NOT be mutated.
    expect(globalStore.autoQueueConfig).toEqual({ enabled: false, strategy: 'related' })
  })

  it('applyRoomAutoQueueAdded prefers payload.state when present', () => {
    const fullState = { songs: [{ id: 'a' }, { id: 'auto' }], current_index: 1, current_song: { id: 'auto' }, status: 'playing', elapsed: 0, queue: [], history: [] }
    globalStore.applyRoomAutoQueueAdded('lobby', {
      room_slug: 'lobby',
      song: { id: 'auto', added_by: 'system:autoqueue' },
      source_song_title: 'a',
      current_index: 1,
      current_song: { id: 'auto' },
      status: 'playing',
      elapsed: 0,
      state: fullState,
    })
    expect(globalStore.roomQueues.lobby.state).toEqual(fullState)
  })

  it('applyRoomAutoQueueAdded fallback: stamps current_index/current_song/status/elapsed', () => {
    globalStore.setRoomQueueState('lobby', { songs: [{ id: 'a' }], current_index: 0, current_song: { id: 'a' }, status: 'playing', elapsed: 9, queue: [], history: [] })
    globalStore.applyRoomAutoQueueAdded('lobby', {
      room_slug: 'lobby',
      song: { id: 'b' },
      current_index: 1,
      current_song: { id: 'b' },
      status: 'playing',
      elapsed: 0,
    })
    const s = globalStore.roomQueues.lobby.state
    expect(s.current_index).toBe(1)
    expect(s.current_song).toEqual({ id: 'b' })
    expect(s.status).toBe('playing')
    expect(s.elapsed).toBe(0)
  })

  it('applyRoomAutoQueueAdded NEVER mutates global queueState / autoQueueConfig / voteSessions / currentUser', () => {
    const before = {
      queueState: JSON.parse(JSON.stringify(globalStore.queueState)),
      voteSessions: { ...globalStore.voteSessions },
      autoQueueConfig: { ...globalStore.autoQueueConfig },
      currentUser: globalStore.currentUser,
    }
    globalStore.applyRoomAutoQueueAdded('lobby', { state: { songs: [{ id: 'auto' }], current_index: 0, current_song: { id: 'auto' }, status: 'playing', elapsed: 0, queue: [], history: [] } })
    globalStore.applyRoomAutoQueueConfigChanged('lobby', { enabled: true, strategy: 'related' })
    expect(globalStore.queueState).toEqual(before.queueState)
    expect(globalStore.voteSessions).toEqual(before.voteSessions)
    expect(globalStore.autoQueueConfig).toEqual(before.autoQueueConfig)
    expect(globalStore.currentUser).toBe(before.currentUser)
  })

  // --- R10c room deletion + member removal mutators ---

  it('_ensureRoomEntry seeds archived/removed/members to {false,false,[]}', () => {
    globalStore.setRoomQueueConnected('lobby', true)
    expect(globalStore.roomQueues.lobby.archived).toBe(false)
    expect(globalStore.roomQueues.lobby.removed).toBe(false)
    expect(globalStore.roomQueues.lobby.members).toEqual([])
  })

  it('markRoomArchived flips archived=true on the per-room entry', () => {
    globalStore.markRoomArchived('lobby')
    expect(globalStore.roomQueues.lobby.archived).toBe(true)
  })

  it('markRoomRemovedAsCurrentUser flips removed=true on the per-room entry', () => {
    globalStore.markRoomRemovedAsCurrentUser('lobby')
    expect(globalStore.roomQueues.lobby.removed).toBe(true)
  })

  it('applyRoomMembersChanged replaces the per-room members list from payload.members', () => {
    globalStore.applyRoomMembersChanged('lobby', {
      room_slug: 'lobby',
      members: [
        { user_id: 1, role: 'host' },
        { user_id: 2, role: 'admin' },
        { user_id: 3, role: 'guest' },
      ],
    })
    expect(globalStore.roomQueues.lobby.members).toEqual([
      { user_id: 1, role: 'host' },
      { user_id: 2, role: 'admin' },
      { user_id: 3, role: 'guest' },
    ])
  })

  it('applyRoomMembersChanged leaves the cached list intact when payload.members is missing', () => {
    globalStore.applyRoomMembersChanged('lobby', {
      members: [{ user_id: 1, role: 'host' }],
    })
    globalStore.applyRoomMembersChanged('lobby', { room_slug: 'lobby' }) // no members key
    expect(globalStore.roomQueues.lobby.members).toEqual([{ user_id: 1, role: 'host' }])
  })

  it('R10c mutators are isolated to roomQueues[slug] and NEVER touch global queueState / autoQueueConfig / voteSessions / currentUser', () => {
    const before = {
      queueState: JSON.parse(JSON.stringify(globalStore.queueState)),
      voteSessions: { ...globalStore.voteSessions },
      autoQueueConfig: { ...globalStore.autoQueueConfig },
      currentUser: globalStore.currentUser,
    }
    globalStore.markRoomArchived('lobby')
    globalStore.markRoomRemovedAsCurrentUser('lobby')
    globalStore.applyRoomMembersChanged('lobby', { members: [{ user_id: 1, role: 'host' }] })
    expect(globalStore.queueState).toEqual(before.queueState)
    expect(globalStore.voteSessions).toEqual(before.voteSessions)
    expect(globalStore.autoQueueConfig).toEqual(before.autoQueueConfig)
    expect(globalStore.currentUser).toBe(before.currentUser)
  })
})

// --- R11a: room chat mutators ---

describe('Room chat store (R11a)', () => {
  beforeEach(() => {
    globalStore.roomQueues = {}
    globalStore.queueState = { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] }
    globalStore.voteSessions = {}
    globalStore.autoQueueConfig = { enabled: false, strategy: 'related' }
  })

  it('_ensureRoomEntry seeds an empty messages array on first access', () => {
    const entry = globalStore._ensureRoomEntry('lobby')
    expect(Array.isArray(entry.messages)).toBe(true)
    expect(entry.messages).toEqual([])
  })

  it('setRoomChatMessages replaces the slice with the seeded list', () => {
    const seeded = [
      { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: 'hi', created_at: '2026-01-01T00:00:00Z' },
      { id: 2, room_slug: 'lobby', sender: { user_id: 2, display_name: 'B' }, content: 'yo', created_at: '2026-01-01T00:00:01Z' },
    ]
    globalStore.setRoomChatMessages('lobby', seeded)
    expect(globalStore.roomQueues.lobby.messages).toEqual(seeded)
  })

  it('setRoomChatMessages is a no-op for null / non-array payloads', () => {
    globalStore.setRoomChatMessages('lobby', [
      { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: 'hi', created_at: 't' },
    ])
    globalStore.setRoomChatMessages('lobby', null)
    globalStore.setRoomChatMessages('lobby', { messages: [] })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
  })

  it('applyRoomChatMessageCreated appends a single message envelope', () => {
    globalStore.applyRoomChatMessageCreated('lobby', {
      message: { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: 'a', created_at: 't1' },
    })
    globalStore.applyRoomChatMessageCreated('lobby', {
      message: { id: 2, room_slug: 'lobby', sender: { user_id: 2, display_name: 'B' }, content: 'b', created_at: 't2' },
    })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(2)
    expect(globalStore.roomQueues.lobby.messages[1].content).toBe('b')
  })

  it('applyRoomChatMessageCreated caps the slice at MaxRoomChatMessages (oldest dropped)', async () => {
    const { MaxRoomChatMessages } = await import('../index')
    // Seed MaxRoomChatMessages + 5 entries; verify the oldest 5 are dropped.
    const total = MaxRoomChatMessages + 5
    for (let i = 0; i < total; i++) {
      globalStore.applyRoomChatMessageCreated('lobby', {
        message: { id: i, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: `m${i}`, created_at: `t${i}` },
      })
    }
    const msgs = globalStore.roomQueues.lobby.messages
    expect(msgs.length).toBe(MaxRoomChatMessages)
    // The oldest retained entry must be id=5 (the 6th inserted).
    expect(msgs[0].id).toBe(5)
    expect(msgs[msgs.length - 1].id).toBe(total - 1)
  })

  it('applyRoomChatMessageCreated is a no-op for missing payload or message', () => {
    globalStore.applyRoomChatMessageCreated('lobby', null)
    globalStore.applyRoomChatMessageCreated('lobby', {})
    // A no-op must NOT poison the local state. Either no entry was
    // created OR the entry's messages array is empty.
    const entry = globalStore.roomQueues.lobby
    if (entry) {
      expect(entry.messages).toEqual([])
    } else {
      expect(entry).toBeUndefined()
    }
  })

  it('chat mutators never touch global queue / vote / autoQueueConfig / currentUser', async () => {
    const before = {
      queueState: JSON.parse(JSON.stringify(globalStore.queueState)),
      voteSessions: { ...globalStore.voteSessions },
      autoQueueConfig: { ...globalStore.autoQueueConfig },
      currentUser: globalStore.currentUser,
    }
    globalStore.applyRoomChatMessageCreated('lobby', {
      message: { id: 1, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content: 'hi', created_at: 't' },
    })
    globalStore.setRoomChatMessages('lobby2', [
      { id: 2, room_slug: 'lobby2', sender: { user_id: 1, display_name: 'A' }, content: 'x', created_at: 't' },
    ])
    expect(globalStore.queueState).toEqual(before.queueState)
    expect(globalStore.voteSessions).toEqual(before.voteSessions)
    expect(globalStore.autoQueueConfig).toEqual(before.autoQueueConfig)
    expect(globalStore.currentUser).toBe(before.currentUser)
  })
})

// --- R11a corrective-pass store tests ---

describe('Room chat store (R11a corrective pass)', () => {
  beforeEach(() => {
    globalStore.roomQueues = {}
    globalStore.queueState = { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] }
    globalStore.voteSessions = {}
    globalStore.autoQueueConfig = { enabled: false, strategy: 'related' }
  })

  function msg(id, content, createdAt) {
    return { id, room_slug: 'lobby', sender: { user_id: 1, display_name: 'A' }, content, created_at: createdAt }
  }

  it('setRoomChatMessages merges by id (REST + WS for the same id → one row)', () => {
    globalStore.applyRoomChatMessageCreated('lobby', { message: msg(1, 'a', '2026-01-01T00:00:00Z') })
    globalStore.setRoomChatMessages('lobby', [
      msg(1, 'a', '2026-01-01T00:00:00Z'),
      msg(2, 'b', '2026-01-01T00:00:01Z'),
    ])
    expect(globalStore.roomQueues.lobby.messages.map((m) => m.id)).toEqual([1, 2])
  })

  it('applyRoomChatMessageCreated via _mergeRoomChat sorts oldest → newest by created_at with id tie-break', () => {
    // Two messages with the same created_at; the id tie-break
    // must order them deterministically.
    globalStore.applyRoomChatMessageCreated('lobby', { message: msg(2, 'b', '2026-01-01T00:00:00Z') })
    globalStore.applyRoomChatMessageCreated('lobby', { message: msg(1, 'a', '2026-01-01T00:00:00Z') })
    expect(globalStore.roomQueues.lobby.messages.map((m) => m.id)).toEqual([1, 2])
  })

  it('applyRoomChatMessageFromPost applies a single message via the same merge path', () => {
    globalStore.applyRoomChatMessageFromPost('lobby', { message: msg(1, 'a', '2026-01-01T00:00:00Z') })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
    // A subsequent WS event for the same id must dedupe.
    globalStore.applyRoomChatMessageCreated('lobby', { message: msg(1, 'a', '2026-01-01T00:00:00Z') })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
  })

  it('merge caps at MaxRoomChatMessages, dropping oldest entries', async () => {
    const { MaxRoomChatMessages } = await import('../index')
    // Seed N+5 entries via WS events; verify only the newest N
    // entries remain. Use a strictly monotonic created_at so the
    // cap is owned by ordering, not by collision within a single
    // wall-clock second.
    const total = MaxRoomChatMessages + 5
    const base = Date.parse('2026-01-01T00:00:00Z')
    for (let i = 0; i < total; i++) {
      globalStore.applyRoomChatMessageCreated('lobby', {
        message: msg(i, `m${i}`, new Date(base + i * 1000).toISOString()),
      })
    }
    const msgs = globalStore.roomQueues.lobby.messages
    expect(msgs.length).toBe(MaxRoomChatMessages)
    // The oldest 5 must be dropped.
    const ids = msgs.map((m) => m.id)
    expect(ids[0]).toBe(5)
    expect(ids[ids.length - 1]).toBe(total - 1)
  })

  it('merge ignores duplicate ids without producing a second row', () => {
    globalStore.applyRoomChatMessageCreated('lobby', { message: msg(1, 'a', '2026-01-01T00:00:00Z') })
    globalStore.applyRoomChatMessageCreated('lobby', { message: msg(1, 'a', '2026-01-01T00:00:00Z') })
    globalStore.applyRoomChatMessageCreated('lobby', { message: msg(1, 'a', '2026-01-01T00:00:00Z') })
    expect(globalStore.roomQueues.lobby.messages.length).toBe(1)
  })
})
