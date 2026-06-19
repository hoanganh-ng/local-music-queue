import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import NowPlaying from '../NowPlaying.vue'
import { globalStore } from '../../../store'

const { mockSetStatus, mockSongEnded } = vi.hoisted(() => ({
  mockSetStatus: vi.fn(() => Promise.resolve()),
  mockSongEnded: vi.fn(() => Promise.resolve()),
}))
vi.mock('../../../services/api', () => ({
  api: {
    setStatus: (...args) => mockSetStatus(...args),
    songEnded: (...args) => mockSongEnded(...args),
    syncPlayback: vi.fn(() => Promise.resolve()),
    prevSong: vi.fn(() => Promise.resolve()),
    changeVolume: vi.fn(() => Promise.resolve()),
  }
}))

// Capture the player-state callback for direct invocation.
let capturedOnStateChange = null

beforeEach(() => {
  vi.useFakeTimers()
  mockSetStatus.mockClear()
  mockSongEnded.mockClear()
  capturedOnStateChange = null
  globalStore.setUser({ id: 1, role: 'host', display_name: 'HostUser' })

  // Minimal YT IFrame API stub. Constructor calls onYouTubeIframeAPIReady → initPlayer,
  // which builds a fake player and stores the state-change handler.
  window.YT = {
    PlayerState: { PLAYING: 1, PAUSED: 2, ENDED: 0, BUFFERING: 3, CUED: 5, UNSTARTED: -1 },
    Player: vi.fn(function (_elId, opts) {
      capturedOnStateChange = opts.events.onStateChange
      this.loadVideoById = vi.fn()
      this.playVideo = vi.fn()
      this.pauseVideo = vi.fn()
      this.stopVideo = vi.fn()
      this.getCurrentTime = () => 0
      this.getVolume = () => 50
      this.setVolume = vi.fn()
    })
  }
})

afterEach(() => {
  vi.useRealTimers()
  delete window.YT
  globalStore.clearUser()
})

function mountHost(initialSong) {
  return mount(NowPlaying, {
    props: {
      currentSong: initialSong,
      status: 'playing',
      isHost: true,
      canControl: true,
    },
    global: {
      stubs: { BaseButton: true, VoteButton: true }
    }
  })
}

// Read the live loadGeneration / settledGeneration pair. NowPlaying does not
// export them, so we inspect behavior through the API surface and through the
// number of times loadVideoById is called for each videoId.
function lastLoadCall(videoId) {
  const player = window.YT.Player.mock.results[window.YT.Player.mock.results.length - 1].value
  return player.loadVideoById.mock.calls.find(c => c[0] === videoId)
}

describe('NowPlaying — Sprint 004 host-player programmatic-load guard (generation-safe)', () => {
  it('cancel a pause debounce scheduled for song A when song B loads', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()
    expect(capturedOnStateChange).toBeTypeOf('function')

    // User pauses A — statusChangeTimeout is armed for setStatus('paused', ...).
    capturedOnStateChange({ data: window.YT.PlayerState.PAUSED })
    // Before the 300 ms debounce fires, a new videoId arrives and the watcher
    // must cancel the pending statusChangeTimeout.
    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    // Advance well past the 300 ms debounce window.
    await vi.advanceTimersByTimeAsync(1000)
    expect(mockSetStatus).not.toHaveBeenCalled()
  })

  it('loading B then C before B settles: B late PLAYING does not clear C guard', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }
    const songC = { id: 'ccccccccccc', title: 'C', url: 'https://youtu.be/ccccccccccc', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()

    // Load B, then immediately load C — neither settles.
    await wrapper.setProps({ currentSong: songB })
    await nextTick()
    await wrapper.setProps({ currentSong: songC })
    await nextTick()

    // B's late PLAYING arrives (e.g. its slow decode completes after C
    // swapped in). The generation must NOT match, so the guard stays armed
    // for C and no setStatus('playing') fires.
    capturedOnStateChange({ data: window.YT.PlayerState.PLAYING })
    await vi.advanceTimersByTimeAsync(1000)

    // No status setStatus calls at all — even B's "playing" event was dropped.
    expect(mockSetStatus).not.toHaveBeenCalled()
    expect(mockSongEnded).not.toHaveBeenCalled()
  })

  it('stale ENDED during a programmatic load does NOT call api.songEnded', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()

    // Programmatic load begins for B.
    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    // A's ENDED arrives late (B is the current target). loadGeneration !==
    // settledGeneration → ENDED must NOT advance the backend.
    capturedOnStateChange({ data: window.YT.PlayerState.ENDED })
    await flushPromises()
    expect(mockSongEnded).not.toHaveBeenCalled()
  })

  it('genuine ENDED for a settled current song calls api.songEnded', async () => {
    const song = { id: 'ddddddddddd', title: 'D', url: 'https://youtu.be/ddddddddddd', added_by: 'Alice' }

    const wrapper = mountHost(song)
    await nextTick()

    // No load in progress: settledGeneration === loadGeneration from init.
    // A genuine PAUSED from host settling flow establishes settled state.
    capturedOnStateChange({ data: window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()

    // Now an ENDED is genuine end-of-media and must reach the backend.
    capturedOnStateChange({ data: window.YT.PlayerState.ENDED })
    await flushPromises()
    expect(mockSongEnded).toHaveBeenCalled()
  })

  it('genuine host pause after settlement still calls api.setStatus("paused", ...)', async () => {
    const song = { id: 'eeeeeeeeeee', title: 'E', url: 'https://youtu.be/eeeeeeeeeee', added_by: 'Alice' }

    const wrapper = mountHost(song)
    await nextTick()

    // No load in progress: a genuine PAUSED from a real host click goes
    // through the 300 ms debounce and reaches the backend.
    capturedOnStateChange({ data: window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
  })

  it('PLAYING matching props.status settles the current generation; subsequent PAUSED is genuine', async () => {
    const song = { id: 'fffffffffff', title: 'F', url: 'https://youtu.be/fffffffffff', added_by: 'Alice' }

    const wrapper = mountHost(song)
    await nextTick()

    // Programmatic swap armed a guard. PLAYING matching props.status='playing'
    // advances settledGeneration, releasing the guard for subsequent genuine
    // events.
    capturedOnStateChange({ data: window.YT.PlayerState.PLAYING })
    await vi.advanceTimersByTimeAsync(100)

    // Now a genuine host pause must reach the backend.
    capturedOnStateChange({ data: window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
  })
})
