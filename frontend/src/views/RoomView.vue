<template>
  <div class="room-view app-container">
    <header class="top-nav glass-panel">
      <div class="brand">
        <h2 class="cyber-glitch">Room: {{ slug }}</h2>
      </div>
      <div class="user-info">
        <span class="user-name">{{ currentUser?.display_name }}</span>
        <button class="logout-btn" @click="handleBack">Back</button>
      </div>
    </header>

    <main class="room-content">
      <section class="queue-panel glass-panel">
        <h3>Queue</h3>
        <p v-if="roomState.lastError" class="error" role="alert">
          {{ roomState.lastError }}
        </p>
        <ol class="queue-list">
          <li
            v-for="(song, idx) in roomState.state.songs || []"
            :key="String(song.id) + ':' + idx"
            class="queue-item"
          >
            <span class="song-title">{{ song.title || '(untitled)' }}</span>
            <button
              v-if="idx !== (roomState.state.current_index ?? -1)"
              class="prioritize-btn"
              :disabled="!canMutate"
              @click="prioritizeSong(idx)"
            >
              Prioritize
            </button>
            <button
              class="remove-btn"
              :disabled="!canMutate"
              @click="removeSong(idx)"
            >
              Remove
            </button>
          </li>
          <li v-if="!(roomState.state.songs || []).length" class="empty">
            Queue is empty.
          </li>
        </ol>
      </section>

      <section class="add-panel glass-panel">
        <input
          v-model="addUrl"
          type="text"
          class="add-input"
          placeholder="Paste a YouTube URL"
          :disabled="!canMutate"
        />
        <button class="add-btn" :disabled="!canMutate || !addUrl" @click="addSong">
          Add song
        </button>
        <button class="clear-btn" :disabled="!canClear" @click="clearQueue">
          Clear queue
        </button>
      </section>

      <section class="playback-panel glass-panel" v-if="hasCurrentSong">
        <h3>Playback</h3>
        <p class="now-playing">
          Now playing: <strong>{{ currentSongTitle }}</strong>
          <span class="elapsed"> · {{ roomState.state.elapsed ?? 0 }}s</span>
        </p>
        <div class="playback-buttons">
          <button
            class="play-btn"
            :disabled="!canMutate || roomState.state.status === 'playing'"
            @click="setPlaybackStatus('playing')"
          >Play</button>
          <button
            class="pause-btn"
            :disabled="!canMutate || roomState.state.status === 'paused'"
            @click="setPlaybackStatus('paused')"
          >Pause</button>
          <button
            class="prev-btn"
            :disabled="!canMutate || !canGoPrevious"
            @click="prevPlayback"
          >Prev</button>
          <button
            class="skip-btn"
            :disabled="!canMutate"
            @click="skipPlayback"
          >Skip</button>
          <button
            class="ended-btn"
            :disabled="!canMutate"
            @click="songEnded"
          >Ended</button>
        </div>
        <div class="volume-row">
          <button
            class="vol-up-btn"
            :disabled="!canMutate"
            @click="changeVolume('up')"
          >Vol +</button>
          <button
            class="vol-down-btn"
            :disabled="!canMutate"
            @click="changeVolume('down')"
          >Vol −</button>
        </div>
        <p class="hint">Only the active lease holder may control playback.</p>
      </section>
    </main>
    <ToastContainer />
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { globalStore } from '../store'
import { api } from '../services/api'
import { createRoomWsClient } from '../services/room-websocket'
import { useToast } from '../composables/useToast'
import ToastContainer from '../components/ui/ToastContainer.vue'

const route = useRoute()
const router = useRouter()
const toast = useToast()

// canMutate / canClear are UI conveniences only. The backend is the source
// of truth — RoomView surfaces 401/403/404/409 cleanly through APIError.
const slug = computed(() => route.params.slug)
const currentUser = computed(() => globalStore.currentUser)
const roomState = computed(() => {
  if (!globalStore.roomQueues[slug.value]) {
    return { state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] }, lastError: null, connected: false, recoveryInFlight: false }
  }
  return globalStore.roomQueues[slug.value]
})
const canMutate = computed(() => !!currentUser.value && roomState.value.connected)
const canClear = computed(() => !!currentUser.value && roomState.value.connected)
const addUrl = ref('')
let wsClient = null
let suppressSeqGap = false
// Track the slug we actually connected to so unmount can clean it up
// even if the route reactive value drifts (test mounts without an initial slug).
let connectedSlug = null

function applyMessage(msg) {
  switch (msg.type) {
    case 'room_queue_sync':
      globalStore.setRoomQueueState(slug.value, msg.data?.state || { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
      break
    case 'room_queue_song_added':
      globalStore.applyRoomSongAdded(slug.value, msg.data?.song, msg.data?.position ?? -1, msg.data?.state)
      break
    case 'room_queue_song_removed':
      globalStore.applyRoomSongRemoved(slug.value, msg.data?.removed_index ?? -1, msg.data?.state)
      break
    case 'room_queue_song_prioritized':
      globalStore.applyRoomSongPrioritized(slug.value, msg.data?.from_index ?? -1, msg.data?.to_index ?? -1, msg.data?.song, msg.data?.state)
      break
    case 'room_queue_cleared':
      globalStore.applyRoomQueueCleared(slug.value, msg.data?.state)
      break
    // R09a: lease-aware per-room playback deltas.
    case 'room_playback_status_changed':
      globalStore.applyRoomPlaybackStatusChanged(slug.value, msg.data)
      break
    case 'room_playback_elapsed_sync':
      globalStore.applyRoomPlaybackElapsedSync(slug.value, msg.data)
      break
    case 'room_playback_song_advanced':
      globalStore.applyRoomPlaybackSongAdvanced(slug.value, msg.data)
      break
    // R09c: lease-aware per-room volume delta. Payload is intentionally
    // minimal (no state snapshot) because volume is not persisted. We
    // surface a toast so other lease holders know a peer changed volume.
    // The current UI does not render a global volume meter; this listener
    // is purely informational.
    case 'room_playback_volume_changed':
      // No store mutation — there is no persisted volume state.
      // UI affordance: a low-priority toast (info, not error).
      // Explicit up/down branch so an unknown payload reads as a
      // neutral change rather than silently defaulting to 'decreased'.
      {
        const d = msg.data?.direction
        const verb = d === 'up' ? 'increased' : d === 'down' ? 'decreased' : 'changed'
        toast.info(`Volume ${verb}.`)
      }
      break
    // R09d: lease-aware per-room previous-playback delta. Payload
    // carries the full post-mutation state snapshot (preferred); the
    // fallback path mirrors entity.Queue.PrevToPrevious semantics.
    case 'room_playback_song_previous':
      globalStore.applyRoomPlaybackSongPrevious(slug.value, msg.data)
      break
    default:
      // Ignore global / unrelated event types per the R07c contract.
      break
  }
}

async function onGap() {
  if (suppressSeqGap) return
  globalStore.setRoomQueueRecoveryInFlight(slug.value, true)
  try {
    const fresh = await api.getRoomQueue(slug.value)
    globalStore.setRoomQueueState(slug.value, fresh)
    globalStore.setRoomQueueSeq(slug.value, 0)
    suppressSeqGap = true
    // Re-enable gap detection on the next inbound message.
    setTimeout(() => { suppressSeqGap = false }, 50)
  } catch (e) {
    const status = e?.status
    const reason = e?.message || ''
    const message = status === 401 ? 'You are signed out. Log in again.'
      : status === 403 ? `You are not a member of this room (${reason || 'forbidden'}).`
      : status === 404 ? 'Room not found.'
      : status === 409 ? 'Room is archived or in conflict.'
      : 'Could not resync room queue.'
    globalStore.setRoomQueueError(slug.value, message)
    toast.error(message)
  } finally {
    globalStore.setRoomQueueRecoveryInFlight(slug.value, false)
  }
}

async function seedStateFromRest() {
  try {
    const state = await api.getRoomQueue(slug.value)
    globalStore.setRoomQueueState(slug.value, state)
    globalStore.setRoomQueueSeq(slug.value, 0)
  } catch (e) {
    const status = e?.status
    if (status === 401) toast.error('You are signed out. Log in again.')
    else if (status === 403) toast.error('You are not a member of this room.')
    else if (status === 404) toast.error('Room not found.')
    else if (status === 409) toast.error('Room is archived or in conflict.')
  }
}

function buildRoomClient(targetSlug) {
  const options = {
    onMessage: applyMessage,
    onGap,
    onOpen: () => globalStore.setRoomQueueConnected(targetSlug, true),
    onClose: () => globalStore.setRoomQueueConnected(targetSlug, false),
    onError: () => globalStore.setRoomQueueConnected(targetSlug, false),
  }
  const client = createRoomWsClient(targetSlug, options)
  // Mirror option callbacks onto the returned client so callers (and tests)
  // can introspect / re-invoke them on the instance itself.
  client.onMessage = options.onMessage
  client.onGap = options.onGap
  client.onOpen = options.onOpen
  client.onClose = options.onClose
  client.onError = options.onError
  return client
}

function teardownCurrentClient() {
  if (wsClient) {
    wsClient.disconnect()
    wsClient = null
  }
  if (connectedSlug != null) {
    globalStore.clearRoomQueueState(connectedSlug)
    connectedSlug = null
  }
}

onMounted(async () => {
  const target = slug.value
  if (target == null) return
  connectedSlug = target
  globalStore.setRoomQueueConnected(target, false)
  await seedStateFromRest()
  wsClient = buildRoomClient(target)
  wsClient.connect()
})

watch(slug, async (newSlug) => {
  if (newSlug == null || newSlug === connectedSlug) return
  teardownCurrentClient()
  connectedSlug = newSlug
  globalStore.setRoomQueueConnected(newSlug, false)
  // Mirror the mount path: REST-seed the new room's queue before opening
  // the room WebSocket so the UI is consistent with the next room's state
  // even if the WS initial sync is delayed.
  await seedStateFromRest()
  wsClient = buildRoomClient(newSlug)
  wsClient.connect()
})

onUnmounted(() => {
  teardownCurrentClient()
})

async function addSong() {
  if (!addUrl.value) return
  try {
    await api.addRoomSong(slug.value, addUrl.value)
    addUrl.value = ''
  } catch (e) {
    const s = e?.status
    if (s === 401) toast.error('You are signed out. Log in again.')
    else if (s === 403) toast.error('You do not have permission to add to this room queue.')
    else if (s === 404) toast.error('Room not found.')
    else if (s === 409) toast.error('That song is already in the room queue.')
    else toast.error('Could not add song to room queue.')
  }
}

async function removeSong(index) {
  try {
    await api.removeRoomSong(slug.value, index)
  } catch (e) {
    const s = e?.status
    if (s === 401) toast.error('You are signed out. Log in again.')
    else if (s === 403) toast.error('You can only remove your own songs (or be the host/admin).')
    else if (s === 404) toast.error('Room not found.')
    else toast.error('Could not remove song from room queue.')
  }
}

async function prioritizeSong(index) {
  try {
    await api.prioritizeRoomSong(slug.value, index)
  } catch (e) {
    const s = e?.status
    if (s === 400) toast.error('Cannot prioritize that song.')
    else if (s === 401) toast.error('You are signed out. Log in again.')
    else if (s === 403) toast.error('Only the host or an admin can prioritize a song.')
    else if (s === 404) toast.error('Room not found.')
    else if (s === 409) toast.error('Room is archived or in conflict.')
    else toast.error('Could not prioritize song.')
  }
}

async function clearQueue() {
  try {
    await api.clearRoomQueue(slug.value)
  } catch (e) {
    const s = e?.status
    if (s === 401) toast.error('You are signed out. Log in again.')
    else if (s === 403) toast.error('Only the host or an admin can clear this queue.')
    else if (s === 404) toast.error('Room not found.')
    else toast.error('Could not clear room queue.')
  }
}

// --- R09a playback controls ---
//
// UI gates are convenience only. The backend enforces lease-holder
// authorisation inside roomqueue.Interactor and surfaces 400/401/403/
// 404/409/410; the toasts below mirror the same status-code mapping
// as the existing R07d prioritize control.

const hasCurrentSong = computed(() => {
  const s = roomState.value.state
  return Array.isArray(s.songs) && typeof s.current_index === 'number' && s.current_index >= 0 && s.current_index < s.songs.length
})
const currentSongTitle = computed(() => {
  const s = roomState.value.state
  if (!hasCurrentSong.value) return ''
  return s.songs[s.current_index]?.title || '(untitled)'
})
// canGoPrevious is a UI convenience only. The backend enforces the
// CurrentIndex > 0 invariant and returns 400 when the move is
// impossible; this gate just suppresses a misleading click affordance
// when the queue is on the first song.
const canGoPrevious = computed(() => {
  const s = roomState.value.state
  return typeof s.current_index === 'number' && s.current_index > 0
})

async function setPlaybackStatus(status) {
  try {
    await api.setRoomPlaybackStatus(slug.value, status)
  } catch (e) {
    mapPlaybackToast(e, 'Could not change playback status.')
  }
}

async function skipPlayback() {
  try {
    await api.skipRoomPlayback(slug.value)
  } catch (e) {
    mapPlaybackToast(e, 'Could not skip the current song.')
  }
}

// R09d: prev playback command. Lease-holder only. Empty body. The
// backend enforces the no-current-song / already-on-first invariants
// and returns 400 without mutation or broadcast on those error paths;
// mapPlaybackToast already handles 400 (existing surface).
async function prevPlayback() {
  try {
    await api.prevRoomPlayback(slug.value)
  } catch (e) {
    mapPlaybackToast(e, 'Could not go to the previous song.')
  }
}

async function songEnded() {
  try {
    await api.roomSongEnded(slug.value)
  } catch (e) {
    mapPlaybackToast(e, 'Could not mark the song as ended.')
  }
}

async function changeVolume(direction) {
  try {
    await api.changeRoomPlaybackVolume(slug.value, direction)
  } catch (e) {
    mapPlaybackToast(e, 'Could not change volume.')
  }
}

function mapPlaybackToast(e, fallback) {
  const s = e?.status
  if (s === 400) toast.error('Playback command is invalid.')
  else if (s === 401) toast.error('You are signed out. Log in again.')
  else if (s === 403) toast.error('Only the active lease holder can control playback.')
  else if (s === 404) toast.error('Room or active lease not found.')
  else if (s === 409) toast.error('Room is archived or in conflict.')
  else if (s === 410) toast.error('Player lease has expired — reclaim to continue.')
  else toast.error(fallback)
}

function handleBack() {
  router.push({ name: 'Dashboard' })
}
</script>

<style scoped>
.room-view {
  padding: 1rem;
  gap: 1rem;
  display: flex;
  flex-direction: column;
}
.top-nav {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 1rem 2rem;
  border-radius: var(--radius-md);
}
.user-info {
  display: flex;
  align-items: center;
  gap: 1rem;
}
.user-name {
  font-weight: 500;
}
.logout-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 0.875rem;
  cursor: pointer;
}
.room-content {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 1rem;
}
.queue-panel,
.add-panel {
  padding: 1rem;
  border-radius: var(--radius-md);
}
.queue-list {
  list-style: decimal;
  padding-left: 1.25rem;
}
.queue-item {
  padding: 0.4rem 0;
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.remove-btn,
.add-btn,
.clear-btn,
.prioritize-btn {
  background: var(--accent);
  color: #0a0a0a;
  border: none;
  border-radius: var(--radius-sm);
  padding: 0.35rem 0.75rem;
  font-weight: 600;
  cursor: pointer;
}
.prioritize-btn {
  margin-right: 0.4rem;
}
.remove-btn:disabled,
.add-btn:disabled,
.clear-btn:disabled,
.prioritize-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* R09d: prev button sits inline with the other playback controls. It
   uses the same accent surface as the queue-mutation buttons and is
   disabled when the queue is on the first song (no prev available) or
   when the actor lacks the lease (UI convenience; backend is
   authoritative). */
.prev-btn {
  background: var(--accent);
  color: #0a0a0a;
  border: none;
  border-radius: var(--radius-sm);
  padding: 0.35rem 0.75rem;
  font-weight: 600;
  cursor: pointer;
  margin-right: 0.4rem;
}
.prev-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.add-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.add-input {
  padding: 0.5rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--text-muted);
  background: transparent;
  color: inherit;
}
.error {
  color: var(--danger);
  font-size: 0.875rem;
}
.empty {
  color: var(--text-muted);
  font-style: italic;
}
</style>
