<template>
  <div class="dashboard-view app-container">
    <header class="top-nav glass-panel">
      <div class="brand">
        <h2>Local Music Queue</h2>
      </div>
      <div class="user-info">
        <span class="user-role" :class="isHost ? 'role-host' : 'role-guest'">
          {{ isHost ? 'Host' : 'Guest' }}
        </span>
        <span class="user-name">{{ currentUser?.display_name }}</span>
        <button class="logout-btn" @click="handleLogout">Exit</button>
      </div>
    </header>

    <main class="dashboard-content">
      <!-- Left Column: Activity Log -->
      <aside class="col-left">
        <ActivityLog :logs="globalStore.queueState.history || []" />
      </aside>

      <!-- Center Column: Player and Input -->
      <section class="col-center">
        <div class="player-wrapper">
          <NowPlaying
            :currentSong="globalStore.queueState.current_song"
            :status="globalStore.queueState.status"
            :isHost="isHost"
            @toggle-playback="togglePlayback"
            @skip="skipSong"
          />
        </div>
        <div class="input-wrapper">
          <SubmitForm ref="submitFormRef" @submit="addSong" />
        </div>
      </section>

      <!-- Right Column: Queue List -->
      <aside class="col-right">
        <QueueList
          :queue="globalStore.queueState.queue || []"
          :isHost="isHost"
          :currentIndex="globalStore.queueState.current_index"
        />
      </aside>
    </main>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { globalStore } from '../store'
import { api } from '../services/api'
import { wsClient } from '../services/websocket'

import ActivityLog from '../components/dashboard/ActivityLog.vue'
import NowPlaying from '../components/dashboard/NowPlaying.vue'
import QueueList from '../components/dashboard/QueueList.vue'
import SubmitForm from '../components/forms/SubmitForm.vue'

const router = useRouter()

const currentUser = computed(() => globalStore.currentUser)
const isHost = computed(() => currentUser.value?.role === 'host')

const submitFormRef = ref(null)
let unsubscribeSongAdded = null

onMounted(async () => {
  // Connect WebSocket
  wsClient.connect()

  unsubscribeSongAdded = wsClient.onSongAdded((song) => {
    if (submitFormRef.value) {
      submitFormRef.value.handleSongAdded(song)
    }
  })

  // Fetch initial queue state
  try {
    const state = await api.getQueue()
    globalStore.updateQueueState(state)
  } catch (e) {
    console.error("Failed to fetch initial queue:", e)
  }
})

onUnmounted(() => {
  if (unsubscribeSongAdded) {
    unsubscribeSongAdded()
  }
  wsClient.disconnect()
})

const handleLogout = () => {
  globalStore.clearUser()
  wsClient.disconnect()
  router.push({ name: 'Auth' })
}

const addSong = async (url, metadata = null) => {
  await api.addSong(url, currentUser.value.display_name, metadata)
}

const togglePlayback = async () => {
  const currentStatus = globalStore.queueState.status
  const newStatus = currentStatus === 'playing' ? 'paused' : 'playing'
  try {
    await api.setStatus(newStatus, currentUser.value.display_name)
  } catch (e) {
    console.error("Failed to set status:", e)
  }
}

const skipSong = async () => {
  try {
    await api.skipSong(currentUser.value.display_name)
  } catch (e) {
    console.error("Failed to skip song:", e)
  }
}
</script>

<style scoped>
.dashboard-view {
  padding: 1rem;
  gap: 1rem;
}

.top-nav {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1rem 2rem;
  border-radius: var(--radius-md);
  margin-bottom: 0.5rem;
}

.brand h2 {
  font-size: 1.25rem;
  margin: 0;
  color: var(--text-main);
  background: linear-gradient(90deg, var(--accent) 0%, var(--accent-hover) 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.user-info {
  display: flex;
  align-items: center;
  gap: 1rem;
}

.user-role {
  font-size: 0.75rem;
  padding: 0.2rem 0.6rem;
  border-radius: var(--radius-sm);
  font-weight: 700;
  text-transform: uppercase;
}

.role-host {
  background: rgba(46, 204, 113, 0.2);
  color: #2ecc71;
  border: 1px solid rgba(46, 204, 113, 0.4);
}

.role-guest {
  background: rgba(149, 165, 166, 0.2);
  color: #95a5a6;
  border: 1px solid rgba(149, 165, 166, 0.4);
}

.user-name {
  font-weight: 500;
  color: var(--text-main);
}

.logout-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 0.875rem;
  cursor: pointer;
  transition: color 0.2s;
}

.logout-btn:hover {
  color: var(--danger);
}

.dashboard-content {
  display: flex;
  flex-grow: 1;
  gap: 1.5rem;
  overflow: hidden; /* prevents stretching */
}

/* 3-Column Layout */
.col-left,
.col-right {
  flex: 1;
  min-width: 250px;
  max-width: 350px;
  display: flex;
  flex-direction: column;
}

.col-center {
  flex: 2;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  min-width: 400px;
}

.player-wrapper {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
}

.input-wrapper {
  flex-shrink: 0;
}

/* Responsive adjustments */
@media (max-width: 1024px) {
  .dashboard-content {
    flex-direction: column;
    overflow-y: auto;
  }
  
  .col-left, .col-right, .col-center {
    max-width: 100%;
    min-width: auto;
  }
  
  /* Give fixed heights to side columns on mobile so they don't grow infinitely */
  .col-left { order: 3; height: 300px; flex: none; }
  .col-center { order: 1; min-height: 500px; }
  .col-right { order: 2; height: 300px; flex: none; }
}
</style>
