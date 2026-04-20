<template>
  <div class="now-playing glass-panel">
    <div class="player-header">
      <h2>Now Playing</h2>
      <div v-if="currentSong" class="status-badge" :class="status">
        {{ status.toUpperCase() }}
      </div>
    </div>

    <div v-if="currentSong" class="song-details">
      <!-- Thumbnail/Visuals -->
      <div class="artwork-container">
        <!-- We can use the default maxresdefault thumbnail from YouTube -->
        <img 
          v-if="!isHost || !showPlayer" 
          :src="thumbnailUrl" 
          alt="Album Art" 
          class="artwork-img"
        />
        
        <!-- Host Only: The actual YouTube IFrame -->
        <div v-if="isHost && showPlayer" class="youtube-wrapper">
          <div id="youtube-player"></div>
        </div>
      </div>

      <div class="song-info">
        <h3 class="song-title">{{ currentSong.title }}</h3>
        <p class="song-meta">Added by: <strong>{{ currentSong.added_by }}</strong></p>
      </div>

      <!-- Controls (Host only) -->
      <div v-if="isHost" class="host-controls">
        <BaseButton 
          variant="secondary" 
          @click="$emit('toggle-playback')"
        >
          <span v-if="status === 'playing'">Pause</span>
          <span v-else>Play</span>
        </BaseButton>
        <BaseButton 
          variant="primary" 
          @click="$emit('skip')"
        >
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
import { computed, watch, onMounted, ref } from 'vue'
import BaseButton from '../ui/BaseButton.vue'

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
  }
})

const emit = defineEmits(['toggle-playback', 'skip', 'song-end'])

// The iframe player instance
let ytPlayer = null
const showPlayer = ref(true)

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
    } else {
      initPlayer()
    }
  }
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
}

function onPlayerStateChange(event) {
  if (event.data === window.YT.PlayerState.ENDED) {
    emit('song-end')
  }
}

// Watch for song changes to update the player
watch(() => videoId.value, (newId, oldId) => {
  if (props.isHost && ytPlayer && ytPlayer.loadVideoById) {
    if (newId) {
      if (newId !== oldId) {
        ytPlayer.loadVideoById(newId)
        if (props.status === 'playing') ytPlayer.playVideo()
      }
    } else {
      ytPlayer.stopVideo()
    }
  }
})

// Watch for status changes (Play/Pause commands from clients)
watch(() => props.status, (newStatus) => {
  if (props.isHost && ytPlayer && ytPlayer.playVideo) {
    if (newStatus === 'playing') {
      ytPlayer.playVideo()
    } else if (newStatus === 'paused') {
      ytPlayer.pauseVideo()
    } else {
      ytPlayer.stopVideo()
    }
  }
})

</script>

<style scoped>
.now-playing {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 2rem;
  overflow: hidden;
}

.player-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 2rem;
}

.player-header h2 {
  font-size: 1.5rem;
  font-weight: 700;
  margin: 0;
  color: var(--accent);
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
  flex-grow: 1;
  gap: 1.5rem;
  align-items: center;
}

.artwork-container {
  width: 100%;
  max-width: 480px;
  aspect-ratio: 16 / 9;
  border-radius: var(--radius-md);
  overflow: hidden;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
  background: #000;
  position: relative;
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
  color: var(--text-main);
  margin-bottom: 0.5rem;
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
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
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
  0% { transform: translateY(0px); }
  50% { transform: translateY(-10px); }
  100% { transform: translateY(0px); }
}
</style>
