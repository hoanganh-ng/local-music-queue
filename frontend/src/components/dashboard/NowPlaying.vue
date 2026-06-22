<template>
  <div class="now-playing glass-panel">
    <div class="player-header">
      <h2 class="cyber-glitch">Now Playing</h2>
      <div v-if="currentSong" class="status-badge" :class="status">
        {{ status.toUpperCase() }}
      </div>
    </div>

    <div v-if="currentSong" class="song-details">
      <!-- Thumbnail/Visuals and Volume Controls -->
      <div class="artwork-section">
        <div class="artwork-container">
          <!-- We can use the default maxresdefault thumbnail from YouTube -->
          <img v-if="!isHost || !showPlayer" :src="thumbnailUrl" alt="Album Art" class="artwork-img" />

          <!-- Host Only: The actual YouTube IFrame -->
          <div v-if="isHost && showPlayer" class="youtube-wrapper">
            <div id="youtube-player"></div>
          </div>
        </div>

        <!-- Volume Controls next to artwork -->
        <div v-if="canControl" class="volume-controls">
          <button
            class="mute-toggle"
            :class="{ muted: isMuted }"
            :aria-label="isMuted ? 'Unmute' : 'Mute'"
            :title="isMuted ? 'Unmute' : 'Mute'"
            @click="toggleMute"
          >
            <svg v-if="isMuted || localVolume === 0" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" width="20" height="20">
              <path d="M3.63 3.63a.996.996 0 000 1.41L7.29 8.7 7 9H4c-.55 0-1 .45-1 1v4c0 .55.45 1 1 1h3l3.29 3.29c.63.63 1.71.18 1.71-.71v-4.17l4.18 4.18c-.49.37-1.02.68-1.6.91-.36.15-.58.53-.58.92 0 .72.73 1.18 1.39.91.8-.33 1.55-.77 2.22-1.31l1.34 1.34a.996.996 0 101.41-1.41L5.05 3.63c-.39-.39-1.02-.39-1.42 0z"/>
            </svg>
            <svg v-else-if="localVolume < 50" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" width="20" height="20">
              <path d="M18.5 12c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM5 9v6h4l5 5V4L9 9H5z"/>
            </svg>
            <svg v-else xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" width="20" height="20">
              <path d="M3 9v6h4l5 5V4L7 9H3zm13.5 3c0-1.77-1.02-3.29-2.5-4.03v8.05c1.48-.73 2.5-2.25 2.5-4.02zM14 3.23v2.06c2.89.86 5 3.54 5 6.71s-2.11 5.85-5 6.71v2.06c4.01-.91 7-4.49 7-8.77s-2.99-7.86-7-8.77z"/>
            </svg>
          </button>
          <div class="slider-group">
            <input
              type="range"
              min="0"
              max="100"
              :value="localVolume"
              class="volume-slider"
              aria-label="Volume"
              @input="handleSliderInput"
              @change="handleSliderCommit"
            />
            <span class="volume-label">{{ localVolume }}%</span>
          </div>
        </div>
      </div>

      <div class="song-info">
        <h3 class="song-title" :title="currentSong.title">{{ currentSong.title }}</h3>
        <p class="song-meta">Added by: <strong>{{ currentSong.added_by }}</strong></p>
      </div>

      <!-- Vote Button (Guest/Admin only) -->
      <div v-if="currentSong && currentUser && currentUser.role !== 'host'" class="vote-section">
        <VoteButton
          voteType="skip"
          :songID="currentSong.id"
          :disabled="currentUser.role === 'host'"
        />
      </div>

      <!-- Controls (Host or Admin) -->
      <div v-if="canControl" class="host-controls">
        <BaseButton variant="secondary" :aria-label="status === 'playing' ? 'Pause playback' : 'Start playback'" @click="$emit('toggle-playback')">
          <span v-if="status === 'playing'">Pause</span>
          <span v-else>Play</span>
        </BaseButton>
        <BaseButton variant="primary" aria-label="Play previous song" @click="handlePrev">
          Previous
        </BaseButton>
        <BaseButton variant="primary" aria-label="Skip current song" @click="$emit('skip')">
          Skip
        </BaseButton>
      </div>
    </div>

    <div v-else class="empty-state">
      <div class="idle-icon">🎵</div>
      <p>Nothing is currently playing.</p>
      <p class="sub-text">Add a song to the queue to get started.</p>
    </div>
  </div>
</template>

<script setup>
import { computed, watch, onMounted, ref, onUnmounted } from 'vue'
import { api } from '../../services/api'
import { globalStore } from '../../store'
import BaseButton from '../ui/BaseButton.vue'
import VoteButton from './VoteButton.vue'

const props = defineProps({
  currentSong: {
    type: Object,
    default: null
  },
  status: {
    type: String,
    default: 'stopped'
  },
  isHost: {
    type: Boolean,
    default: false
  },
  canControl: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['toggle-playback', 'skip', 'song-end'])

const currentUser = computed(() => globalStore.currentUser)

// --- Volume state ---
const localVolume = ref(50)
const previousNonZeroVolume = ref(50)
const isMuted = ref(false)
// For non-host remote: track last assumed volume to compute deltas.
let lastAssumedRemoteVolume = 50
let remoteThrottleTimer = null
const REMOTE_THROTTLE_MS = 200

function clampVolume(v) {
  return Math.max(0, Math.min(100, Math.round(v)))
}

// The iframe player instance
let ytPlayer = null
const showPlayer = ref(true)
let syncInterval = null
const isUpdatingFromProp = ref(false)
let isUpdatingFromPropResetTimer = null
let statusChangeTimeout = null
// Sprint 004 (generation-safe transitions): every videoId change arms a fresh
// monotonic `loadGeneration`. A callback (PAUSED/BUFFERING/PLAYING/ENDED) that
// arrives with a generation older than the current one belongs to a previous
// load and must not affect the new song. The safety timer attempts settlement
// through the same identity, player-state, and authoritative-status checks as
// the normal confirmation path.
let loadGeneration = 0
let settledGeneration = 0
let loadingTimeout = null
let settleConfirmationTimer = null
let settleConfirmationGeneration = 0
let endConfirmationTimer = null
let endConfirmationGeneration = null
let songEndedIssuedGeneration = null
const LOADING_GUARD_MS = 1500
const SETTLE_CONFIRM_MS = 250
const END_CONFIRM_MS = 250

// Track the videoId the component currently considers "live". Any state-change
// callback whose reported videoId does not match this is a stale callback for
// a previous load and must not affect the current transition (must NOT
// settle, must NOT schedule setStatus, must NOT call songEnded).
const expectedVideoId = ref(null)
// Hard kill-switch: callbacks that arrive after unmount must do nothing.
const isUnmounted = ref(false)

// Helper to extract Video ID from URL
const videoId = computed(() => {
  if (!props.currentSong || !props.currentSong.url) return null;
  // Simple regex to grab the ID from standard youtube urls
  const match = props.currentSong.url.match(/(?:v=|\/)([0-9A-Za-z_-]{11}).*/);
  return match ? match[1] : null;
})

const thumbnailUrl = computed(() => {
  if (!videoId.value) return 'https://via.placeholder.com/640x360.png?text=No+Cover'
  return `https://img.youtube.com/vi/${videoId.value}/maxresdefault.jpg`
})

// Initialize YouTube Iframe API if host
onMounted(() => {
  if (props.isHost) {
    if (!window.YT) {
      const tag = document.createElement('script')
      tag.src = "https://www.youtube.com/iframe_api"
      const firstScriptTag = document.getElementsByTagName('script')[0]
      firstScriptTag.parentNode.insertBefore(tag, firstScriptTag)

      window.onYouTubeIframeAPIReady = initPlayer
      setPausedWhenInitPlayer()
    } else {
      initPlayer()
    }
  }
})

onUnmounted(() => {
  if (syncInterval) clearInterval(syncInterval)
  if (statusChangeTimeout) clearTimeout(statusChangeTimeout)
  statusChangeTimeout = null
  if (loadingTimeout) clearTimeout(loadingTimeout)
  loadingTimeout = null
  if (settleConfirmationTimer) clearTimeout(settleConfirmationTimer)
  settleConfirmationTimer = null
  if (endConfirmationTimer) clearTimeout(endConfirmationTimer)
  endConfirmationTimer = null
  endConfirmationGeneration = null
  songEndedIssuedGeneration = null
  if (isUpdatingFromPropResetTimer) clearTimeout(isUpdatingFromPropResetTimer)
  isUpdatingFromPropResetTimer = null
  isUpdatingFromProp.value = false
  if (remoteThrottleTimer) clearTimeout(remoteThrottleTimer)
  remoteThrottleTimer = null
  // Hard kill-switch: any in-flight YT callback must be a no-op.
  isUnmounted.value = true
  // Invalidate any in-flight load callbacks and clear the settled marker so
  // no later event handler can reach api.* through this component.
  loadGeneration++
  settledGeneration++
  expectedVideoId.value = null
})

function onPlayerReady() {
  // Initialize local volume from the real YouTube iframe player.
  const v = readHostVolume()
  if (v !== null) {
    localVolume.value = v
    if (v > 0) previousNonZeroVolume.value = v
    lastAssumedRemoteVolume = v
  }
}

function initPlayer() {
  // Sprint 004 (identity-keyed): seed expectedVideoId BEFORE constructing
  // the player so any state-change callback that fires during the initial
  // loadVideoById() passes the identity check.
  expectedVideoId.value = videoId.value || null
  ytPlayer = new window.YT.Player('youtube-player', {
    height: '100%',
    width: '100%',
    videoId: videoId.value || '',
    playerVars: {
      'autoplay': props.status === 'playing' ? 1 : 0,
      'controls': 1,
      'disablekb': 1
    },
    events: {
      'onReady': onPlayerReady,
      'onStateChange': onPlayerStateChange
    }
  })

  // Start sync interval for playback time
  if (syncInterval) clearInterval(syncInterval)
  syncInterval = setInterval(() => {
    if (ytPlayer && ytPlayer.getCurrentTime && props.status === 'playing') {
      const elapsed = Math.floor(ytPlayer.getCurrentTime())
      api.syncPlayback(elapsed).catch(err => console.error('Sync failed:', err))
    }
  }, 5000)
}

// Read the player's currently loaded videoId by parsing getVideoUrl(). This
// is the authoritative seam for the real YouTube IFrame API: event.target
// identifies the player, not an arbitrary historical video event, and the
// player's own state is the only stable source of "which video is loaded
// right now". The mock player implements getVideoUrl/getPlayerState.
function readPlayerVideoId() {
  if (!ytPlayer) return null
  if (typeof ytPlayer.getVideoUrl !== 'function') return null
  try {
    const url = ytPlayer.getVideoUrl() || ''
    const match = url.match(/(?:v=|\/)([0-9A-Za-z_-]{11})/)
    return match ? match[1] : null
  } catch (_) {
    return null
  }
}

function readPlayerState() {
  if (!ytPlayer || typeof ytPlayer.getPlayerState !== 'function') return null
  try {
    return ytPlayer.getPlayerState()
  } catch (_) {
    return null
  }
}

function trySettleGeneration(expectedGeneration) {
  if (isUnmounted.value) return false
  if (expectedGeneration !== loadGeneration) return false
  if (readPlayerVideoId() !== expectedVideoId.value) return false
  if (readPlayerState() !== window.YT.PlayerState.PLAYING) return false
  if (props.status !== 'playing') return false

  settledGeneration = expectedGeneration
  if (loadingTimeout) {
    clearTimeout(loadingTimeout)
    loadingTimeout = null
  }
  return true
}

function clearEndConfirmation() {
  if (endConfirmationTimer) {
    clearTimeout(endConfirmationTimer)
    endConfirmationTimer = null
  }
  endConfirmationGeneration = null
}

function resetEndGuardsForNewGeneration() {
  clearEndConfirmation()
  songEndedIssuedGeneration = null
}

function confirmSongEnded(expectedGeneration) {
  endConfirmationTimer = null

  if (isUnmounted.value) return
  if (expectedGeneration !== loadGeneration) return
  if (expectedGeneration !== settledGeneration) return
  if (endConfirmationGeneration !== expectedGeneration) return
  endConfirmationGeneration = null
  if (songEndedIssuedGeneration === expectedGeneration) return
  if (readPlayerVideoId() !== expectedVideoId.value) return
  if (readPlayerState() !== window.YT.PlayerState.ENDED) return

  songEndedIssuedGeneration = expectedGeneration
  api.songEnded()
    .then(() => {
      // Backend will broadcast status change via WebSocket
      // The prop watcher will update the player state
    })
    .catch(err => console.error('Song ended call failed:', err))
}

function scheduleSongEndedConfirmation(expectedGeneration) {
  if (expectedGeneration !== settledGeneration) return
  if (endConfirmationGeneration === expectedGeneration) return
  if (songEndedIssuedGeneration === expectedGeneration) return

  clearEndConfirmation()
  endConfirmationGeneration = expectedGeneration
  endConfirmationTimer = setTimeout(() => {
    confirmSongEnded(expectedGeneration)
  }, END_CONFIRM_MS)
}

function onPlayerStateChange(event) {
  // Hard kill-switch: any callback arriving after unmount must do nothing.
  if (isUnmounted.value) return
  // Prevent loop: skip if change came from prop watcher
  if (isUpdatingFromProp.value) return

  // Sprint 004 (identity-keyed): the player's currently loaded video is the
  // source of truth for which video this callback belongs to. event.target
  // identifies the player itself, not a per-event video — we must read the
  // player's current video via getVideoUrl(), not a synthetic per-callback
  // videoId. Mismatches are stale callbacks for a previous load and must NOT
  // settle, must NOT schedule setStatus, must NOT call songEnded.
  const playerCurrentVideoId = readPlayerVideoId()
  if (playerCurrentVideoId !== expectedVideoId.value) {
    return
  }

  const state = event.data

  if (state === window.YT.PlayerState.ENDED) {
    scheduleSongEndedConfirmation(loadGeneration)
    return
  }

  // Sprint 004 (identity-confirmed generation-safe): while a programmatic
  // loadVideoById is settling, the YT API fires transient
  // PAUSED/BUFFERING/PLAYING events that do not reflect host intent. A
  // matching PLAYING (state + props.status + expected videoId) arms a
  // short confirmation. Settled generation only advances at confirmation
  // time when the player is still mounted, the generation has not changed,
  // the player's currently loaded video still matches, and the player
  // still reports PLAYING.
  if (loadGeneration !== settledGeneration) {
    if (
      state === window.YT.PlayerState.PLAYING &&
      props.status === 'playing'
    ) {
      // Arm a generation-keyed confirmation. A stale callback that
      // arrived after the load moved on cannot settle the transition
      // because the confirmation will re-check identity, state, and
      // generation at fire time.
      if (settleConfirmationTimer) clearTimeout(settleConfirmationTimer)
      settleConfirmationGeneration = loadGeneration
      settleConfirmationTimer = setTimeout(() => {
        settleConfirmationTimer = null
        trySettleGeneration(settleConfirmationGeneration)
      }, SETTLE_CONFIRM_MS)
    }
    return
  }

  let newStatus = null
  if (state === window.YT.PlayerState.PLAYING) {
    newStatus = 'playing'
  } else if (state === window.YT.PlayerState.PAUSED) {
    newStatus = 'paused'
  } else {
    return
  }

  // Sprint 004 (cancel-on-every-transition): clear and null any existing
  // statusChangeTimeout FIRST for every accepted PLAYING/PAUSED callback,
  // then schedule a new timeout only when the new player state differs
  // from props.status. This ensures that a sequence of transient state
  // changes during a programmatic load cannot strand a stale paused
  // debounce that would later fire against the new song.
  if (statusChangeTimeout) {
    clearTimeout(statusChangeTimeout)
    statusChangeTimeout = null
  }
  if (newStatus !== props.status) {
    statusChangeTimeout = setTimeout(() => {
      statusChangeTimeout = null
      api.setStatus(newStatus, globalStore.currentUser.display_name)
        .catch(err => console.error('Status sync failed:', err))
    }, 300)
  }
}

async function handlePrev() {
  try {
    await api.prevSong(globalStore.currentUser.display_name)
  } catch (err) {
    console.error('Previous song failed:', err)
  }
}

function setHostVolume(vol) {
  if (ytPlayer && typeof ytPlayer.setVolume === 'function') {
    ytPlayer.setVolume(vol)
  }
}

function readHostVolume() {
  if (ytPlayer && typeof ytPlayer.getVolume === 'function') {
    try { return clampVolume(ytPlayer.getVolume()) } catch (_) { /* ignore */ }
  }
  return null
}

async function sendRemoteVolumeDelta(direction) {
  try {
    await api.changeVolume(direction)
  } catch (err) {
    console.error('Volume change failed:', err)
  }
}

function handleSliderInput(event) {
  const val = clampVolume(Number(event.target.value))
  localVolume.value = val
  if (props.isHost) {
    // Host: apply immediately to the YouTube iframe.
    setHostVolume(val)
    if (val > 0) {
      isMuted.value = false
      previousNonZeroVolume.value = val
    }
  }
  // Non-host: only update the slider visually; commit on @change.
}

function handleSliderCommit(event) {
  const val = clampVolume(Number(event.target.value))
  localVolume.value = val
  if (props.isHost) {
    setHostVolume(val)
    if (val > 0) {
      isMuted.value = false
      previousNonZeroVolume.value = val
    }
  } else {
    // Non-host: send the minimum number of ±10 direction commands to
    // approximate the desired volume through the existing contract.
    const diff = val - lastAssumedRemoteVolume
    const steps = Math.round(diff / 10)
    if (steps !== 0) {
      const direction = steps > 0 ? 'up' : 'down'
      const count = Math.abs(steps)
      // Throttle: if a burst is already scheduled, coalesce into it.
      if (remoteThrottleTimer) {
        clearTimeout(remoteThrottleTimer)
        remoteThrottleTimer = null
      }
      remoteThrottleTimer = setTimeout(async () => {
        remoteThrottleTimer = null
        for (let i = 0; i < count; i++) {
          await sendRemoteVolumeDelta(direction)
        }
        lastAssumedRemoteVolume = val
      }, REMOTE_THROTTLE_MS)
    }
  }
}

function toggleMute() {
  if (props.isHost) {
    if (isMuted.value || localVolume.value === 0) {
      // Unmute: restore previous non-zero volume.
      const restore = previousNonZeroVolume.value > 0 ? previousNonZeroVolume.value : 50
      localVolume.value = restore
      setHostVolume(restore)
      isMuted.value = false
    } else {
      // Mute: remember current volume, set to 0.
      previousNonZeroVolume.value = localVolume.value > 0 ? localVolume.value : previousNonZeroVolume.value
      localVolume.value = 0
      setHostVolume(0)
      isMuted.value = true
    }
  } else {
    // Non-host: approximate mute/unmute through direction commands.
    // This is best-effort; exact remote mute is not supported by the
    // direction-only contract.
    if (isMuted.value || localVolume.value === 0) {
      const restore = previousNonZeroVolume.value > 0 ? previousNonZeroVolume.value : 50
      const diff = restore - lastAssumedRemoteVolume
      const steps = Math.round(diff / 10)
      if (steps !== 0) {
        const direction = steps > 0 ? 'up' : 'down'
        for (let i = 0; i < Math.abs(steps); i++) {
          sendRemoteVolumeDelta(direction)
        }
        lastAssumedRemoteVolume = restore
      }
      localVolume.value = restore
      isMuted.value = false
    } else {
      previousNonZeroVolume.value = localVolume.value
      const steps = Math.round(localVolume.value / 10)
      for (let i = 0; i < steps; i++) {
        sendRemoteVolumeDelta('down')
      }
      lastAssumedRemoteVolume = 0
      localVolume.value = 0
      isMuted.value = true
    }
  }
}

async function setPausedWhenInitPlayer() {
  await api.setStatus('paused', globalStore.currentUser.display_name)
}

// Watch for song changes to update the player
watch(() => videoId.value, (newId, oldId) => {
  if (props.isHost && ytPlayer && ytPlayer.loadVideoById) {
    if (newId) {
      if (newId !== oldId) {
        // Sprint 004 (identity-keyed generation-safe): pin the expected
        // current videoId BEFORE arming the generation so callbacks that
        // arrive mid-swap can be filtered by their reported videoId.
        expectedVideoId.value = newId
        // Sprint 004 (generation-safe): arm a fresh load generation. Any
        // callback from a previous load (older generation) cannot affect the
        // new song. Cancel any pending statusChangeTimeout so a stale PAUSED
        // debounce cannot fire after a new load begins. Cancel any pending
        // settle confirmation keyed to the previous generation.
        loadGeneration++
        if (statusChangeTimeout) {
          clearTimeout(statusChangeTimeout)
          statusChangeTimeout = null
        }
        if (settleConfirmationTimer) {
          clearTimeout(settleConfirmationTimer)
          settleConfirmationTimer = null
        }
        resetEndGuardsForNewGeneration()
        if (loadingTimeout) clearTimeout(loadingTimeout)
        const expectedGeneration = loadGeneration
        loadingTimeout = setTimeout(() => {
          trySettleGeneration(expectedGeneration)
          loadingTimeout = null
        }, LOADING_GUARD_MS)

        ytPlayer.loadVideoById(newId)
        if (props.status === 'playing') ytPlayer.playVideo()
      }
    } else {
      // No video to load: stopVideo also fires transient events; guard them.
      expectedVideoId.value = null
      loadGeneration++
      if (statusChangeTimeout) {
        clearTimeout(statusChangeTimeout)
        statusChangeTimeout = null
      }
      if (settleConfirmationTimer) {
        clearTimeout(settleConfirmationTimer)
        settleConfirmationTimer = null
      }
      resetEndGuardsForNewGeneration()
      if (loadingTimeout) clearTimeout(loadingTimeout)
      const expectedGeneration = loadGeneration
      loadingTimeout = setTimeout(() => {
        trySettleGeneration(expectedGeneration)
        loadingTimeout = null
      }, LOADING_GUARD_MS)
      ytPlayer.stopVideo()
    }
  }
})

// Watch for status changes (Play/Pause commands from clients)
watch(() => props.status, (newStatus) => {
  if (props.isHost && ytPlayer && ytPlayer.playVideo) {
    isUpdatingFromProp.value = true

    if (newStatus === 'playing') {
      ytPlayer.playVideo()
    } else if (newStatus === 'paused') {
      ytPlayer.pauseVideo()
    } else {
      ytPlayer.stopVideo()
    }

    // Sprint 004: store the reset timer handle so subsequent status
    // changes (or unmount) can replace/clear it deterministically rather
    // than racing anonymous timeouts.
    if (isUpdatingFromPropResetTimer) clearTimeout(isUpdatingFromPropResetTimer)
    isUpdatingFromPropResetTimer = setTimeout(() => {
      isUpdatingFromProp.value = false
      isUpdatingFromPropResetTimer = null
    }, 500)
  }
})

// Watch for volume change events from WebSocket (admin remote control)
watch(() => globalStore.queueState.volumeChangeTimestamp, () => {
  if (props.isHost && ytPlayer && ytPlayer.getVolume) {
    const direction = globalStore.queueState.volumeChangeDirection
    if (direction === 'up') {
      const currentVolume = ytPlayer.getVolume()
      const newVolume = Math.min(100, currentVolume + 10)
      ytPlayer.setVolume(newVolume)
      localVolume.value = newVolume
      if (newVolume > 0) {
        isMuted.value = false
        previousNonZeroVolume.value = newVolume
      }
    } else if (direction === 'down') {
      const currentVolume = ytPlayer.getVolume()
      const newVolume = Math.max(0, currentVolume - 10)
      ytPlayer.setVolume(newVolume)
      localVolume.value = newVolume
      if (newVolume === 0) {
        isMuted.value = true
      } else {
        previousNonZeroVolume.value = newVolume
      }
    }
  }
})

</script>

<style scoped>
.now-playing {
  display: flex;
  flex-direction: column;
  padding: 1.5rem;
  overflow: hidden;
}

.player-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1.5rem;
}

.player-header h2 {
  font-size: 1.5rem;
  font-weight: 900;
  margin: 0;
}

.status-badge {
  padding: 0.25rem 0.75rem;
  border-radius: var(--radius-full);
  font-size: 0.75rem;
  font-weight: 600;
  letter-spacing: 1px;
}

.status-badge.playing {
  background: rgba(46, 204, 113, 0.2);
  color: #2ecc71;
  border: 1px solid rgba(46, 204, 113, 0.4);
}

.status-badge.paused {
  background: rgba(241, 196, 15, 0.2);
  color: #f1c40f;
  border: 1px solid rgba(241, 196, 15, 0.4);
}

.status-badge.stopped {
  background: rgba(149, 165, 166, 0.2);
  color: #95a5a6;
  border: 1px solid rgba(149, 165, 166, 0.4);
}

.song-details {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  align-items: center;
}

.artwork-section {
  display: flex;
  gap: 1rem;
  align-items: center;
  width: 100%;
  justify-content: center;
}

.artwork-container {
  width: 100%;
  max-width: 400px;
  aspect-ratio: 16 / 9;
  border-radius: var(--radius-md);
  overflow: hidden;
  box-shadow: var(--glow-cyan), 0 18px 40px rgba(0, 0, 0, 0.72);
  background: #000;
  position: relative;
  clip-path: var(--cyber-chamfer);
  border: 1px solid rgba(0, 212, 255, 0.35);
}

.artwork-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: transform 0.5s ease;
}

.artwork-container:hover .artwork-img {
  transform: scale(1.05);
}

.artwork-container::after {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: repeating-linear-gradient(0deg, rgba(255, 255, 255, 0.05) 0 1px, transparent 1px 5px);
}

.youtube-wrapper {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
}

.song-info {
  text-align: center;
  width: 100%;
}

.song-title {
  font-size: 1.5rem;
  font-weight: 700;
  color: var(--accent);
  margin-bottom: 0.5rem;
  text-shadow: var(--glow-sm);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.song-meta {
  font-size: 0.875rem;
  color: var(--text-muted);
}

.song-meta strong {
  color: var(--accent-hover);
}

.host-controls {
  display: flex;
  gap: 1rem;
  margin-top: 1rem;
  flex-wrap: wrap;
  justify-content: center;
}

.volume-controls {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  background: rgba(0, 0, 0, 0.3);
  border-radius: var(--radius-sm);
  border: 1px solid rgba(0, 212, 255, 0.2);
}

.mute-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  padding: 0;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--accent-hover);
  cursor: pointer;
  transition: color 0.15s ease, background 0.15s ease;
  flex-shrink: 0;
}

.mute-toggle:hover {
  background: rgba(0, 212, 255, 0.12);
}

.mute-toggle.muted {
  color: var(--text-muted);
}

.mute-toggle svg {
  pointer-events: none;
}

.slider-group {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex: 1;
  min-width: 0;
}

.volume-slider {
  -webkit-appearance: none;
  appearance: none;
  width: 100%;
  min-width: 60px;
  max-width: 120px;
  height: 4px;
  border-radius: 2px;
  background: rgba(0, 212, 255, 0.25);
  outline: none;
  cursor: pointer;
}

.volume-slider::-webkit-slider-thumb {
  -webkit-appearance: none;
  appearance: none;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--accent);
  border: 2px solid rgba(0, 255, 136, 0.6);
  cursor: pointer;
  box-shadow: 0 0 6px rgba(0, 255, 136, 0.4);
}

.volume-slider::-moz-range-thumb {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--accent);
  border: 2px solid rgba(0, 255, 136, 0.6);
  cursor: pointer;
  box-shadow: 0 0 6px rgba(0, 255, 136, 0.4);
}

.volume-slider:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.volume-label {
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--text-muted);
  min-width: 3ch;
  text-align: right;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  color: var(--text-muted);
}

.idle-icon {
  font-size: 4rem;
  margin-bottom: 1rem;
  opacity: 0.5;
  animation: float 3s ease-in-out infinite;
}

.sub-text {
  font-size: 0.875rem;
  opacity: 0.7;
  margin-top: 0.5rem;
}

@keyframes float {
  0% {
    transform: translateY(0px);
  }

  50% {
    transform: translateY(-10px);
  }

  100% {
    transform: translateY(0px);
  }
}
</style>
