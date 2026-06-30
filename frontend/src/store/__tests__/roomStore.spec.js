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
})
