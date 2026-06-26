<template>
  <div class="dashboard-view app-container">
    <header class="top-nav glass-panel">
      <div class="brand">
        <h2 class="cyber-glitch">Local Music Queue</h2>
      </div>
      <div class="user-info">
        <span class="connection-badge" :class="connectionBadgeClass">
          <span aria-live="polite">{{ connectionLabel }}</span>
        </span>
        <span class="user-role" :class="roleBadgeClass">
          {{ roleBadgeLabel }}
        </span>
        <button
          v-if="canControl"
          class="radio-mode-toggle"
          :class="{ active: autoQueueEnabled }"
          @click="toggleAutoQueue"
          :title="autoQueueEnabled ? 'Radio Mode: ON' : 'Radio Mode: OFF'"
        >
          <span class="toggle-icon">📻</span>
          <span class="toggle-label">{{ autoQueueEnabled ? 'Radio: ON' : 'Radio: OFF' }}</span>
          <span class="toggle-switch">
            <span class="toggle-slider"></span>
          </span>
        </button>
        <button
          v-if="sessionValid"
          type="button"
          class="copy-token-btn"
          @click="handleCopySessionToken"
          title="Copy your session token to paste into the browser extension Options"
        >
          <span class="copy-token-label">Copy token for extension</span>
        </button>
        <span class="user-name">{{ currentUser?.display_name }}</span>
        <button class="logout-btn" @click="handleLogout">Exit</button>
      </div>
    </header>

    <div
      v-if="voteNotifications.showCta.value"
      class="browser-notif-cta glass-panel"
      role="region"
      aria-label="Enable browser notifications"
    >
      <span class="cta-icon" aria-hidden="true">🔔</span>
      <span class="cta-message">
        Enable browser notifications to see vote updates when this tab is in the background.
      </span>
      <button
        class="cta-enable-btn"
        @click="onEnableBrowserNotifications"
      >
        Enable
      </button>
      <button
        class="cta-dismiss-btn"
        @click="voteNotifications.dismissCta"
        aria-label="Dismiss notification prompt"
      >
        ×
      </button>
    </div>

    <main class="dashboard-content">

      <!-- Left Column: Up Next only -->
      <aside class="col-left">
        <div class="up-next-panel">
          <QueueList
            :queue="globalStore.queueState.queue || []"
            :isHost="isHost"
            :canControl="canControl"
            :currentIndex="globalStore.queueState.current_index"
          />
        </div>
      </aside>

      <!-- Center Column: Now Playing + Add Song below playback -->
      <section class="col-center">
        <div class="now-playing-card">
          <NowPlaying
            :currentSong="globalStore.queueState.current_song"
            :status="globalStore.queueState.status"
            :isHost="isHost"
            :canControl="canControl"
            @toggle-playback="togglePlayback"
            @skip="skipSong"
          />
        </div>
        <div class="add-track-panel">
          <SubmitForm ref="submitFormRef" :onSubmit="addSong" />
        </div>
      </section>

      <!-- Right Column: Activity Log -->
      <aside class="col-right">
        <ActivityLog :logs="globalStore.queueState.history || []" :currentUserName="currentUser?.display_name" />
      </aside>
    </main>
    <ToastContainer />
    <ConfirmDialog />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { globalStore } from '../store'
import { api } from '../services/api'
import { wsClient } from '../services/websocket'
import { sessionHelper } from '../services/session'

import ActivityLog from '../components/dashboard/ActivityLog.vue'
import NowPlaying from '../components/dashboard/NowPlaying.vue'
import QueueList from '../components/dashboard/QueueList.vue'
import SubmitForm from '../components/forms/SubmitForm.vue'
import ToastContainer from '../components/ui/ToastContainer.vue'
import ConfirmDialog from '../components/ui/ConfirmDialog.vue'
import { useToast } from '../composables/useToast'
import { useVoteBrowserNotifications } from '../composables/useVoteBrowserNotifications'

const router = useRouter()
const toast = useToast()
const voteNotifications = useVoteBrowserNotifications()

const currentUser = computed(() => globalStore.currentUser)
const isHost = computed(() => currentUser.value?.role === 'host')
const canControl = computed(() => ['host', 'admin'].includes(currentUser.value?.role))

const roleBadgeClass = computed(() => {
  const role = currentUser.value?.role
  if (role === 'host') return 'role-host'
  if (role === 'admin') return 'role-admin'
  return 'role-guest'
})

const roleBadgeLabel = computed(() => {
  const role = currentUser.value?.role
  if (role === 'host') return 'Host'
  if (role === 'admin') return 'Admin'
  return 'Guest'
})

const connectionLabel = computed(() => {
  const status = globalStore.connectionStatus
  if (status === 'connected') return 'Connected'
  if (status === 'connecting') return 'Connecting'
  if (status === 'reconnecting') return 'Reconnecting'
  return 'Offline'
})

const connectionBadgeClass = computed(() => `connection-${globalStore.connectionStatus}`)

const submitFormRef = ref(null)
const autoQueueEnabled = ref(false)
let unsubscribeSongAdded = null
let unsubscribeVoteEvent = null
const shownVoteNotifications = new Set()

function voteCount(session) {
  return Object.keys(session?.voted_by || {}).length
}

function voteNotificationKey(event) {
  const data = event.data || {}
  if (event.type === 'vote_updated') {
    const session = data.session || {}
    return [
      event.type,
      session.id,
      session.created_at,
      voteCount(session),
    ].join('|')
  }

  return [
    event.type,
    data.session_id,
    data.outcome,
    data.activity?.timestamp,
  ].join('|')
}

function handleVoteEvent(event) {
  const data = event.data || {}
  if (event.type === 'vote_updated' && data.initial_sync === true) {
    return
  }

  const description = data.activity?.description
  if (!description) {
    return
  }

  const key = voteNotificationKey(event)
  if (shownVoteNotifications.has(key)) {
    return
  }
  shownVoteNotifications.add(key)

  // Optional browser notification (additive; toast path below unchanged).
  let notifTitle = 'Vote update'
  if (event.type === 'vote_resolved') {
    notifTitle = data.outcome === 'passed' ? 'Vote passed' : 'Vote expired'
  }
  const notifBody = description.length > 120 ? description.slice(0, 117) + '...' : description
  voteNotifications.notifyVote(key, notifTitle, notifBody)

  if (event.type === 'vote_resolved' && data.outcome === 'passed') {
    toast.success(description)
    return
  }

  toast.info(description)
}

onMounted(async () => {
  // Connect WebSocket
  wsClient.connect()

  unsubscribeSongAdded = wsClient.onSongAdded((song) => {
    if (submitFormRef.value) {
      submitFormRef.value.handleSongAdded(song)
    }
  })

  unsubscribeVoteEvent = wsClient.onVoteEvent(handleVoteEvent)

  // Fetch initial queue state
  try {
    const state = await api.getQueue()
    globalStore.updateQueueState(state)
  } catch (e) {
    console.error("Failed to fetch initial queue:", e)
    toast.error('Could not load queue. Check backend connection.')
  }

  // Fetch auto-queue status
  if (canControl.value) {
    try {
      const status = await api.getAutoQueueStatus()
      autoQueueEnabled.value = status.enabled
    } catch (e) {
      console.error("Failed to fetch auto-queue status:", e)
    }
  }
})

// Watch for auto-queue config changes from WebSocket (other clients toggling)
watch(() => globalStore.autoQueueConfig.enabled, (newEnabled) => {
  if (canControl.value) {
    autoQueueEnabled.value = newEnabled
  }
})

onUnmounted(() => {
  if (unsubscribeSongAdded) {
    unsubscribeSongAdded()
  }
  if (unsubscribeVoteEvent) {
    unsubscribeVoteEvent()
  }
  wsClient.disconnect()
})

const handleLogout = () => {
  sessionHelper.clearSession()
  globalStore.clearUser()
  wsClient.disconnect()
  router.push({ name: 'Auth' })
}

const sessionValid = computed(() => sessionHelper.isValid())

async function handleCopySessionToken() {
  if (!sessionHelper.isValid()) {
    toast.info('No active session. Log in again to copy a fresh token for the extension.')
    return
  }
  const token = sessionHelper.getToken()
  try {
    await navigator.clipboard.writeText(token)
    toast.success('Session token copied. Paste it into the browser extension Options.')
  } catch (err) {
    console.error('Clipboard write failed:', err)
    toast.error('Could not copy to clipboard. Use DevTools → Application → Local Storage → lmq_session_token as a fallback.')
  }
}

defineExpose({ handleCopySessionToken })

const addSong = async (url, metadata = null) => {
  await api.addSong(url, currentUser.value.display_name, currentUser.value.id, metadata)
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

const toggleAutoQueue = async () => {
  try {
    const newEnabled = !autoQueueEnabled.value
    await api.setAutoQueueEnabled(newEnabled)
    autoQueueEnabled.value = newEnabled
  } catch (e) {
    console.error("Failed to toggle auto-queue:", e)
    toast.error('Could not update radio mode.')
  }
}

function onEnableBrowserNotifications() {
  voteNotifications.requestPermission()
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
  border-color: rgba(0, 212, 255, 0.34);
}

.brand h2 {
  font-size: 1.25rem;
  margin: 0;
}

.user-info {
  display: flex;
  align-items: center;
  gap: 1rem;
}

.connection-badge,
.user-role {
  font-size: 0.75rem;
  padding: 0.2rem 0.6rem;
  border-radius: var(--radius-sm);
  font-weight: 700;
  text-transform: uppercase;
}

.connection-connected {
  background: rgba(46, 204, 113, 0.18);
  color: var(--success);
  border: 1px solid rgba(46, 204, 113, 0.4);
}

.connection-connecting,
.connection-reconnecting {
  background: rgba(243, 156, 18, 0.18);
  color: var(--warning);
  border: 1px solid rgba(243, 156, 18, 0.4);
}

.connection-disconnected {
  background: rgba(239, 35, 60, 0.16);
  color: var(--danger);
  border: 1px solid rgba(239, 35, 60, 0.4);
}

.role-host {
  background: rgba(46, 204, 113, 0.2);
  color: #2ecc71;
  border: 1px solid rgba(46, 204, 113, 0.4);
}

.role-admin {
  background: rgba(230, 126, 34, 0.2);
  color: #e67e22;
  border: 1px solid rgba(230, 126, 34, 0.4);
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

.copy-token-btn {
  background: rgba(62, 166, 255, 0.12);
  color: var(--accent-hover);
  border: 1px solid rgba(62, 166, 255, 0.35);
  border-radius: var(--radius-sm);
  padding: 0.35rem 0.75rem;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  cursor: pointer;
  transition: background 0.2s ease, border-color 0.2s ease;
}

.copy-token-btn:hover {
  background: rgba(62, 166, 255, 0.22);
  border-color: var(--accent-hover);
}

.radio-mode-toggle {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  background: rgba(0, 212, 255, 0.08);
  color: var(--accent-hover);
  border: 1px solid rgba(0, 212, 255, 0.35);
  border-radius: var(--radius-sm);
  padding: 0.35rem 0.75rem;
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s ease;
}

.radio-mode-toggle:hover {
  background: rgba(149, 165, 166, 0.15);
  border-color: #95a5a6;
}

.radio-mode-toggle.active {
  background: rgba(0, 255, 136, 0.14);
  color: var(--accent);
  border-color: rgba(0, 255, 136, 0.55);
}

.radio-mode-toggle.active:hover {
  background: rgba(46, 204, 113, 0.2);
  border-color: #2ecc71;
}

.toggle-icon {
  font-size: 1rem;
  line-height: 1;
}

.toggle-label {
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.toggle-switch {
  position: relative;
  width: 32px;
  height: 16px;
  background: rgba(0, 212, 255, 0.3);
  border-radius: 8px;
  transition: background 0.3s ease;
}

.radio-mode-toggle.active .toggle-switch {
  background: rgba(46, 204, 113, 0.5);
}

.toggle-slider {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 12px;
  height: 12px;
  background: var(--accent-hover);
  border-radius: 50%;
  transition: all 0.3s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
}

.radio-mode-toggle.active .toggle-slider {
  transform: translateX(16px);
  background: var(--accent);
}

.browser-notif-cta {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.6rem 1rem;
  border-radius: var(--radius-md);
  border-color: rgba(0, 212, 255, 0.34);
  background: rgba(0, 212, 255, 0.08);
  font-size: 0.875rem;
  color: var(--text-main);
}

.cta-icon {
  font-size: 1.1rem;
  line-height: 1;
}

.cta-message {
  flex: 1 1 auto;
  min-width: 0;
}

.cta-enable-btn {
  background: var(--accent);
  color: #0a0a0a;
  border: none;
  border-radius: var(--radius-sm);
  padding: 0.35rem 0.9rem;
  font-size: 0.8rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  cursor: pointer;
  transition: background 0.2s ease;
}

.cta-enable-btn:hover {
  background: var(--accent-hover);
}

.cta-dismiss-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 1.25rem;
  line-height: 1;
  cursor: pointer;
  padding: 0.1rem 0.4rem;
  transition: color 0.2s ease;
}

.cta-dismiss-btn:hover {
  color: var(--text-main);
}

.dashboard-content {
  display: grid;
  grid-template-columns: minmax(280px, 1fr) minmax(440px, 1.6fr) minmax(280px, 1fr);
  flex-grow: 1;
  min-height: 0;
  gap: 1.25rem;
  overflow: hidden;
}

.col-left,
.col-right,
.col-center {
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.col-left {
  gap: 1rem;
}

.col-center {
  gap: 1rem;
}

.col-center > .now-playing-card {
  flex: 1 1 auto;
  min-height: 0;
}

.col-center > .add-track-panel {
  flex: 0 0 auto;
}

.add-track-panel,
.up-next-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.up-next-panel {
  flex: 1 1 auto;
}

.add-track-panel {
  flex: 0 0 auto;
  position: relative;
  z-index: 20;
  overflow: visible;
}

/* Responsive adjustments */
@media (max-width: 1024px) {
  .dashboard-content {
    display: flex;
    flex-direction: column;
    overflow-y: auto;
  }

  .top-nav {
    align-items: flex-start;
    flex-direction: column;
    gap: 1rem;
  }

  .user-info {
    flex-wrap: wrap;
    gap: 0.75rem;
    width: 100%;
  }

  .col-left, .col-right, .col-center {
    max-width: 100%;
    min-width: auto;
  }

  .col-center { order: 1; }
  .col-left   { order: 2; min-height: min(60vh, 520px); }
  .col-right  { order: 3; min-height: 260px; }
}

@media (max-width: 600px) {
  .dashboard-view {
    padding: 0.75rem;
  }

  .top-nav {
    padding: 1rem;
  }

  .toggle-label,
  .user-name {
    max-width: 9rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}
</style>
