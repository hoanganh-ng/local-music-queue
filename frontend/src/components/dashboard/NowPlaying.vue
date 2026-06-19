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
          <BaseButton variant="secondary" @click="handleVolumeUp" title="Volume Up" aria-label="Volume up">
            Vol +
          </BaseButton>
          <BaseButton variant="secondary" @click="handleVolumeDown" title="Volume Down" aria-label="Volume down">
            Vol -
          </BaseButton>
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

// The iframe player instance
let ytPlayer = null
const showPlayer = ref(true)
let syncInterval = null
const isUpdatingFromProp = ref(false)
let statusChangeTimeout = null
// Sprint 004 (generation-safe transitions): every videoId change arms a fresh
// monotonic `loadGeneration`. A callback (PAUSED/BUFFERING/PLAYING/ENDED) that
// arrives with a generation older than the current one belongs to a previous
// load and must not affect the new song. The safety timer clears the guard if
// no matching PLAYING settles within LOADING_GUARD_MS.
let loadGeneration = 0
let settledGeneration = 0
let loadingTimeout = null
const LOADING_GUARD_MS = 1500

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
  if (loadingTimeout) clearTimeout(loadingTimeout)
  // Invalidate any in-flight load callbacks and clear the settled marker so
  // no later event handler can reach api.* through this component.
  loadGeneration++
  settledGeneration++
})

function initPlayer() {
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

function onPlayerStateChange(event) {
  // Prevent loop: skip if change came from prop watcher
  if (isUpdatingFromProp.value) return

  const state = event.data

  if (state === window.YT.PlayerState.ENDED) {
    // Sprint 004 (generation-safe): only treat ENDED as genuine end-of-media
    // when it matches the currently settled generation. An ENDED arriving while
    // a newer videoId has been swapped in (or during the swap itself) belongs
    // to the previous video and must NOT advance the backend.
    if (loadGeneration === settledGeneration) {
      api.songEnded()
        .then(() => {
          // Backend will broadcast status change via WebSocket
          // The prop watcher will update the player state
        })
        .catch(err => console.error('Song ended call failed:', err))
    }
    return
  }

  // Sprint 004 (generation-safe): while a programmatic loadVideoById is
  // settling, the YT API fires transient PAUSED/BUFFERING/PLAYING events that
  // do not reflect host intent. Suppress the backend status call for those.
  // The settledGeneration is only advanced on a stable PLAYING matching
  // props.status, so an older-generation late PLAYING cannot clear the guard.
  if (loadGeneration !== settledGeneration) {
    if (
      state === window.YT.PlayerState.PLAYING &&
      props.status === 'playing'
    ) {
      settledGeneration = loadGeneration
      if (loadingTimeout) {
        clearTimeout(loadingTimeout)
        loadingTimeout = null
      }
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

  // Debounce status changes
  if (newStatus && newStatus !== props.status) {
    if (statusChangeTimeout) clearTimeout(statusChangeTimeout)
    statusChangeTimeout = setTimeout(() => {
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

async function handleVolumeUp() {
  if (props.isHost && ytPlayer && ytPlayer.getVolume) {
    const currentVolume = ytPlayer.getVolume()
    const newVolume = Math.min(100, currentVolume + 10)
    ytPlayer.setVolume(newVolume)
  } else {
    try {
      await api.changeVolume('up')
    } catch (err) {
      console.error('Volume up failed:', err)
    }
  }
}

async function handleVolumeDown() {
  if (props.isHost && ytPlayer && ytPlayer.getVolume) {
    const currentVolume = ytPlayer.getVolume()
    const newVolume = Math.max(0, currentVolume - 10)
    ytPlayer.setVolume(newVolume)
  } else {
    try {
      await api.changeVolume('down')
    } catch (err) {
      console.error('Volume down failed:', err)
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
        // Sprint 004 (generation-safe): arm a fresh load generation. Any
        // callback from a previous load (older generation) cannot affect the
        // new song. Cancel any pending statusChangeTimeout so a stale PAUSED
        // debounce cannot fire after a new load begins.
        loadGeneration++
        if (statusChangeTimeout) {
          clearTimeout(statusChangeTimeout)
          statusChangeTimeout = null
        }
        if (loadingTimeout) clearTimeout(loadingTimeout)
        loadingTimeout = setTimeout(() => {
          // Safety: if no matching PLAYING settled within the guard window,
          // advance settledGeneration so further callbacks are evaluated
          // against the current state instead of being silently dropped.
          if (loadGeneration > settledGeneration) {
            settledGeneration = loadGeneration
          }
          loadingTimeout = null
        }, LOADING_GUARD_MS)

        ytPlayer.loadVideoById(newId)
        if (props.status === 'playing') ytPlayer.playVideo()
      }
    } else {
      // No video to load: stopVideo also fires transient events; guard them.
      loadGeneration++
      if (statusChangeTimeout) {
        clearTimeout(statusChangeTimeout)
        statusChangeTimeout = null
      }
      if (loadingTimeout) clearTimeout(loadingTimeout)
      loadingTimeout = setTimeout(() => {
        if (loadGeneration > settledGeneration) {
          settledGeneration = loadGeneration
        }
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

    // Reset flag after a short delay to allow player state to settle
    setTimeout(() => {
      isUpdatingFromProp.value = false
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
    } else if (direction === 'down') {
      const currentVolume = ytPlayer.getVolume()
      const newVolume = Math.max(0, currentVolume - 10)
      ytPlayer.setVolume(newVolume)
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
  flex-direction: column;
  gap: 0.5rem;
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
