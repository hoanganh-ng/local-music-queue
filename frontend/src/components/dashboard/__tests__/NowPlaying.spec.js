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
      // Sprint 004 (identity-keyed): the real YouTube IFrame API exposes
      // getVideoUrl() and getPlayerState() on the player. event.target
      // identifies the player, not an arbitrary historical video. The
      // production code reads the player's currently loaded video via
      // getVideoUrl(); the test mutates `currentVideoId` (and `currentState`)
      // to drive the player's reported identity.
      // Seed from the constructor's videoId option so the initial mount
      // already reports a loaded video (mirrors the real YT IFrame which
      // sets videoId from the player config option, not from a later
      // loadVideoById call).
      this.currentVideoId = (opts && opts.videoId) || ''
      this.currentState = this.currentVideoId
        ? window.YT.PlayerState.PLAYING
        : window.YT.PlayerState.UNSTARTED
      this.getVideoUrl = () => {
        if (!this.currentVideoId) return ''
        return `https://www.youtube.com/watch?v=${this.currentVideoId}`
      }
      this.getPlayerState = () => this.currentState
      this.loadVideoById = vi.fn((id) => {
        this.currentVideoId = id
        this.currentState = window.YT.PlayerState.PLAYING
      })
      this.playVideo = vi.fn(() => { this.currentState = window.YT.PlayerState.PLAYING })
      this.pauseVideo = vi.fn(() => { this.currentState = window.YT.PlayerState.PAUSED })
      this.stopVideo = vi.fn(() => {
        this.currentVideoId = ''
        this.currentState = window.YT.PlayerState.UNSTARTED
      })
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

// Returns the most recently constructed mock player.
function latestPlayer() {
  return window.YT.Player.mock.results[window.YT.Player.mock.results.length - 1].value
}

// Drive a state-change callback. The production code reads identity from the
// player's getVideoUrl() and getPlayerState(), so callers either pre-set the
// player's state to the desired target or pass a pre-mutator to set it
// before the callback fires. event.target is a single stable reference to
// the player itself, not a fabricated per-callback identity.
function fireState(stateCode, prepare) {
  if (typeof prepare === 'function') prepare(latestPlayer())
  capturedOnStateChange({ data: stateCode, target: latestPlayer() })
}

describe('NowPlaying — Sprint 004 host-player programmatic-load guard (generation-safe)', () => {
  it('cancel a pause debounce scheduled for song A when song B loads', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()
    expect(capturedOnStateChange).toBeTypeOf('function')

    // User pauses A — statusChangeTimeout is armed for setStatus('paused', ...).
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentState = window.YT.PlayerState.PAUSED })
    // Before the 300 ms debounce fires, a new videoId arrives and the watcher
    // must cancel the pending statusChangeTimeout.
    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    // Advance well past the 300 ms debounce window.
    await vi.advanceTimersByTimeAsync(1000)
    expect(mockSetStatus).not.toHaveBeenCalled()
  })

  // Finding 1: settled current video emits PAUSED → PLAYING within 300 ms →
  // advance past 300 ms → no setStatus('paused') call.
  it('settled current video: PAUSED then PLAYING within 300 ms cancels the paused setStatus', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()

    // Settled initial mount: a PAUSED fires while props.status='playing' →
    // a 300 ms debounce for setStatus('paused') is armed.
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentState = window.YT.PlayerState.PAUSED })
    // Within the 300 ms debounce window a recovery PLAYING fires.
    await vi.advanceTimersByTimeAsync(50)
    fireState(window.YT.PlayerState.PLAYING, (p) => { p.currentState = window.YT.PlayerState.PLAYING })

    // Advance well past 300 ms.
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    // Recovery must cancel the pending paused setStatus.
    expect(mockSetStatus).not.toHaveBeenCalledWith('paused', 'HostUser')
    expect(mockSetStatus).not.toHaveBeenCalled()
  })

  // Finding 1: a PAUSED with no recovery still synchronizes exactly once.
  it('PAUSED with no recovery still synchronizes setStatus("paused") exactly once', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()

    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentState = window.YT.PlayerState.PAUSED })
    // Several repeated PAUSED callbacks (host UI bouncing, etc.) within the
    // 300 ms debounce must NOT each schedule their own setStatus call. The
    // component must clear and null the existing timeout first, then schedule
    // a new one only when the new state differs from props.status.
    await vi.advanceTimersByTimeAsync(50)
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentState = window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(50)
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentState = window.YT.PlayerState.PAUSED })

    // Advance past the 300 ms debounce.
    await vi.advanceTimersByTimeAsync(400)
    await flushPromises()

    expect(mockSetStatus).toHaveBeenCalledTimes(1)
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
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
    fireState(window.YT.PlayerState.PLAYING, (p) => { p.currentVideoId = 'bbbbbbbbbbb'; p.currentState = window.YT.PlayerState.PLAYING })
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

    // Programmatic load begins for B (current player is now B).
    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    // A's late ENDED callback tries to set the player back to A and report
    // ENDED. Identity check (player's getVideoUrl() === B's expectedVideoId)
    // must reject it. → ENDED must NOT advance the backend.
    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentVideoId = 'aaaaaaaaaaa'; p.currentState = window.YT.PlayerState.ENDED })
    await flushPromises()
    expect(mockSongEnded).not.toHaveBeenCalled()
  })

  it('genuine ENDED for a settled current song calls api.songEnded', async () => {
    const song = { id: 'ddddddddddd', title: 'D', url: 'https://youtu.be/ddddddddddd', added_by: 'Alice' }

    const wrapper = mountHost(song)
    await nextTick()

    // No load in progress: settledGeneration === loadGeneration from init.
    // Now an ENDED on the current player reaches the backend after
    // confirmation.
    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(mockSongEnded).toHaveBeenCalled()
  })

  it('genuine host pause after settlement still calls api.setStatus("paused", ...)', async () => {
    const song = { id: 'eeeeeeeeeee', title: 'E', url: 'https://youtu.be/eeeeeeeeeee', added_by: 'Alice' }

    const wrapper = mountHost(song)
    await nextTick()

    // No load in progress: a genuine PAUSED from a real host click goes
    // through the 300 ms debounce and reaches the backend.
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentState = window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
  })

  it('PLAYING matching props.status settles via generation-keyed confirmation; subsequent PAUSED is genuine', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songC = { id: 'ccccccccccc', title: 'C', url: 'https://youtu.be/ccccccccccc', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()
    // Trigger a programmatic swap; generation advances, guard is armed.
    await wrapper.setProps({ currentSong: songC })
    await nextTick()

    // Matching PLAYING for C arms the generation-keyed confirmation timer.
    fireState(window.YT.PlayerState.PLAYING, (p) => { p.currentVideoId = 'ccccccccccc'; p.currentState = window.YT.PlayerState.PLAYING })
    // Wait past the 250 ms confirmation window — the timer re-checks
    // identity + state + generation before settling.
    await vi.advanceTimersByTimeAsync(300)

    // Now a genuine host pause must reach the backend.
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentState = window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
  })

  it('confirmation timer does NOT settle when the player has already moved on (identity mismatch at fire time)', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }
    const songC = { id: 'ccccccccccc', title: 'C', url: 'https://youtu.be/ccccccccccc', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()

    // Load B, then C — neither settles immediately.
    await wrapper.setProps({ currentSong: songB })
    await nextTick()
    await wrapper.setProps({ currentSong: songC })
    await nextTick()

    // A stale PLAYING callback (identity points to B) arrives while the
    // current player is already on C. The handler's top-of-function
    // identity check returns early because the player's getVideoUrl()
    // reports C, not B. B's confirmation must never be armed and the
    // generation must not advance.
    fireState(window.YT.PlayerState.PLAYING, (p) => {
      p.currentVideoId = 'ccccccccccc'
      p.currentState = window.YT.PlayerState.PLAYING
    })

    // Before the LOADING_GUARD_MS safety timer expires, the player state
    // transitions to PAUSED (e.g. user pause). The generation is still
    // not settled, so this PAUSED must not reach the backend.
    fireState(window.YT.PlayerState.PAUSED, (p) => {
      p.currentVideoId = 'ccccccccccc'
      p.currentState = window.YT.PlayerState.PAUSED
    })

    // Advance well past 250 ms confirmation and 300 ms debounce but
    // within the 1500 ms loading guard.
    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    expect(mockSetStatus).not.toHaveBeenCalled()
    expect(mockSongEnded).not.toHaveBeenCalled()
  })

  it('confirmation timer DOES settle when identity and state still match at fire time', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songC = { id: 'ccccccccccc', title: 'C', url: 'https://youtu.be/ccccccccccc', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()
    await wrapper.setProps({ currentSong: songC })
    await nextTick()

    // Matching PLAYING for C arms the confirmation.
    fireState(window.YT.PlayerState.PLAYING, (p) => { p.currentVideoId = 'ccccccccccc'; p.currentState = window.YT.PlayerState.PLAYING })
    // Player state remains PLAYING on C → confirmation settles at fire time.
    await vi.advanceTimersByTimeAsync(300)

    // Subsequent genuine host PAUSED for C reaches the backend.
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentVideoId = 'ccccccccccc'; p.currentState = window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
  })

  it('B then C: late PLAYING(B) and transient PAUSED(C) do not call setStatus or songEnded', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }
    const songC = { id: 'ccccccccccc', title: 'C', url: 'https://youtu.be/ccccccccccc', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()

    // Load B, then C — neither settles.
    await wrapper.setProps({ currentSong: songB })
    await nextTick()
    await wrapper.setProps({ currentSong: songC })
    await nextTick()

    // Late PLAYING for B (B's slow decode completed after C swapped in).
    fireState(window.YT.PlayerState.PLAYING, (p) => { p.currentVideoId = 'bbbbbbbbbbb'; p.currentState = window.YT.PlayerState.PLAYING })
    // Transient PAUSED for C arriving while C's PLAYING hasn't settled yet.
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentVideoId = 'ccccccccccc'; p.currentState = window.YT.PlayerState.PAUSED })

    // Advance past the 300 ms debounce and the 1500 ms loading guard.
    await vi.advanceTimersByTimeAsync(2000)
    expect(mockSetStatus).not.toHaveBeenCalled()
    expect(mockSongEnded).not.toHaveBeenCalled()
  })

  it('matching PLAYING(C) settles; subsequent genuine PAUSED(C) reaches backend', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songC = { id: 'ccccccccccc', title: 'C', url: 'https://youtu.be/ccccccccccc', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()
    await wrapper.setProps({ currentSong: songC })
    await nextTick()

    // Matching PLAYING for C arms the generation-keyed confirmation.
    fireState(window.YT.PlayerState.PLAYING, (p) => { p.currentVideoId = 'ccccccccccc'; p.currentState = window.YT.PlayerState.PLAYING })
    // Wait past the 250 ms confirmation window.
    await vi.advanceTimersByTimeAsync(300)

    // Now a genuine host PAUSED for C goes through the 300 ms debounce.
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentVideoId = 'ccccccccccc'; p.currentState = window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
  })

  it('safety timeout does not settle a still-paused load; later genuine PAUSED syncs only after confirmed PLAYING', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()

    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    // Do not emit PLAYING confirmation. The safety path must re-check the
    // current player state and leave the load guarded while B is still paused.
    latestPlayer().currentVideoId = 'bbbbbbbbbbb'
    latestPlayer().currentState = window.YT.PlayerState.PAUSED
    await vi.advanceTimersByTimeAsync(1600)

    fireState(window.YT.PlayerState.PAUSED, (p) => {
      p.currentVideoId = 'bbbbbbbbbbb'
      p.currentState = window.YT.PlayerState.PAUSED
    })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()

    expect(mockSetStatus).not.toHaveBeenCalled()

    fireState(window.YT.PlayerState.PLAYING, (p) => {
      p.currentVideoId = 'bbbbbbbbbbb'
      p.currentState = window.YT.PlayerState.PLAYING
    })
    await vi.advanceTimersByTimeAsync(300)

    fireState(window.YT.PlayerState.PAUSED, (p) => {
      p.currentVideoId = 'bbbbbbbbbbb'
      p.currentState = window.YT.PlayerState.PAUSED
    })
    await vi.advanceTimersByTimeAsync(350)
    await flushPromises()

    expect(mockSetStatus).toHaveBeenCalledTimes(1)
    expect(mockSetStatus).toHaveBeenCalledWith('paused', 'HostUser')
  })

  it('stale ENDED with wrong videoId does not call api.songEnded', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()
    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    // A's ENDED arrives late, the player is on B. Identity mismatch drops it.
    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentVideoId = 'aaaaaaaaaaa'; p.currentState = window.YT.PlayerState.ENDED })
    await flushPromises()
    expect(mockSongEnded).not.toHaveBeenCalled()
  })

  it('matching ENDED for the current settled videoId calls api.songEnded', async () => {
    const songD = { id: 'ddddddddddd', title: 'D', url: 'https://youtu.be/ddddddddddd', added_by: 'Alice' }

    const wrapper = mountHost(songD)
    await nextTick()

    // Initial mount is already settled (loadGeneration === settledGeneration).
    // An ENDED with matching identity reaches the backend after confirmation.
    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(mockSongEnded).toHaveBeenCalled()
  })

  it('ENDED callback does not call api.songEnded when player reports PLAYING at confirmation time', async () => {
    const songD = { id: 'ddddddddddd', title: 'D', url: 'https://youtu.be/ddddddddddd', added_by: 'Alice' }

    const wrapper = mountHost(songD)
    await nextTick()

    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })
    latestPlayer().currentState = window.YT.PlayerState.PLAYING

    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(mockSongEnded).not.toHaveBeenCalled()
  })

  it('ENDED callback does not call api.songEnded when generation changes before confirmation', async () => {
    const songA = { id: 'aaaaaaaaaaa', title: 'A', url: 'https://youtu.be/aaaaaaaaaaa', added_by: 'Alice' }
    const songB = { id: 'bbbbbbbbbbb', title: 'B', url: 'https://youtu.be/bbbbbbbbbbb', added_by: 'Alice' }

    const wrapper = mountHost(songA)
    await nextTick()

    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })
    await wrapper.setProps({ currentSong: songB })
    await nextTick()

    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(mockSongEnded).not.toHaveBeenCalled()
  })

  it('repeated matching ENDED callbacks for one generation issue exactly one songEnded request', async () => {
    const songD = { id: 'ddddddddddd', title: 'D', url: 'https://youtu.be/ddddddddddd', added_by: 'Alice' }

    const wrapper = mountHost(songD)
    await nextTick()

    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })
    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })
    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })

    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()

    expect(mockSongEnded).toHaveBeenCalledTimes(1)
  })

  it('after unmount, ENDED and PAUSED do not trigger any API calls', async () => {
    const songE = { id: 'eeeeeeeeeee', title: 'E', url: 'https://youtu.be/eeeeeeeeeee', added_by: 'Alice' }

    const wrapper = mountHost(songE)
    await nextTick()
    wrapper.unmount()
    await nextTick()

    // Any callback arriving after unmount must be a no-op.
    fireState(window.YT.PlayerState.ENDED, (p) => { p.currentState = window.YT.PlayerState.ENDED })
    fireState(window.YT.PlayerState.PAUSED, (p) => { p.currentState = window.YT.PlayerState.PAUSED })
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    expect(mockSetStatus).not.toHaveBeenCalled()
    expect(mockSongEnded).not.toHaveBeenCalled()
  })
})
