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

describe('NowPlaying — Sprint 004 host-player programmatic-load guard', () => {
  it('suppresses transient PAUSED/BUFFERING during loadVideoById', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    // Player created synchronously by onMounted via initPlayer
    await nextTick()
    expect(window.YT.Player).toHaveBeenCalled()
    expect(capturedOnStateChange).toBeTypeOf('function')

    // Programmatic load: swap to song B
    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    // YT API fires transient PAUSED then BUFFERING before settling on PLAYING.
    capturedOnStateChange({ data: window.YT.PlayerState.PAUSED })
    capturedOnStateChange({ data: window.YT.PlayerState.BUFFERING })

    // Advance past the 300 ms debounce window.
    await vi.advanceTimersByTimeAsync(500)
    expect(mockSetStatus).not.toHaveBeenCalled()

    // Stable PLAYING event matching props.status clears the guard.
    capturedOnStateChange({ data: window.YT.PlayerState.PLAYING })
    await vi.advanceTimersByTimeAsync(500)
    expect(mockSetStatus).not.toHaveBeenCalled()
  })

  it('still synchronizes genuine host pause after the guard releases', async () => {
    const song = { id: 'cccccccccccc', title: 'C', url: 'https://youtu.be/cccccccccccc', added_by: 'Alice' }
    const wrapper = mountHost(song)
    await nextTick()
    expect(capturedOnStateChange).toBeTypeOf('function')

    // No load in progress: a genuine PAUSED from a real host click goes
    // through the 300 ms debounce and reaches the backend.
    capturedOnStateChange({ data: window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
  })

  it('routes ENDED to api.songEnded even while guard is engaged', async () => {
    const songA = { id: 'ddddddddddd', title: 'D', url: 'https://youtu.be/ddddddddddd', added_by: 'Alice' }
    const songB = { id: 'eeeeeeeeeee', title: 'E', url: 'https://youtu.be/eeeeeeeeeee', added_by: 'Alice' }
    const wrapper = mountHost(songA)
    await nextTick()

    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    // ENDED is real end-of-media, not a transient — it must always notify backend.
    capturedOnStateChange({ data: window.YT.PlayerState.ENDED })
    await flushPromises()
    expect(mockSongEnded).toHaveBeenCalled()
  })
})
