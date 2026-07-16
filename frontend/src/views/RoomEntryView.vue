<template>
  <div class="room-entry-view app-container">
    <header class="top-nav glass-panel">
      <div class="brand">
        <h2 class="cyber-glitch">Rooms</h2>
      </div>
      <div class="user-info">
        <span class="user-name">{{ currentUser?.display_name }}</span>
        <button
          type="button"
          class="dashboard-btn"
          data-testid="dashboard-btn"
          @click="handleBack"
        >
          Back to dashboard
        </button>
      </div>
    </header>

    <main class="entry-content">
      <!-- Active rooms list. Refreshing is independent of the create /
           manual-open / invite-redeem flows; a slow older refresh
           response MUST NOT overwrite a newer one, so every refresh
           records the generation it was started in and only applies
           the result if its generation is still current. -->
      <section class="panel glass-panel" data-testid="active-rooms-panel">
        <header class="panel-header">
          <h3>Active rooms</h3>
          <button
            type="button"
            class="refresh-btn"
            data-testid="refresh-rooms-btn"
            :disabled="listInFlight"
            @click="refreshActiveRooms"
          >Refresh</button>
        </header>
        <p class="panel-hint">
          Open one of the active rooms below. If the room you want is
          missing, ask the host for an invite token — joining a room
          requires an invite.
        </p>
        <p
          v-if="listError"
          class="error"
          role="alert"
          data-testid="list-error"
        >
          {{ listError }}
        </p>
        <ul
          v-if="activeRooms.length"
          class="room-list"
          data-testid="room-list"
        >
          <li
            v-for="room in activeRooms"
            :key="room.id"
            class="room-item"
            :data-testid="`room-item-${room.slug}`"
          >
            <span class="room-name">{{ room.name }}</span>
            <span class="room-slug">{{ room.slug }}</span>
            <button
              type="button"
              class="open-btn"
              :data-testid="`open-room-${room.slug}`"
              @click="openListedRoom(room)"
            >Open</button>
          </li>
        </ul>
        <p
          v-else-if="!listInFlight && !listError"
          class="empty"
          data-testid="room-list-empty"
        >
          No active rooms yet. Create one below or redeem an invite.
        </p>
        <p
          v-else-if="listInFlight"
          class="loading"
          data-testid="room-list-loading"
        >
          Loading…
        </p>
      </section>

      <!-- Manual open by slug. The form calls getRoom() first so the
           UI can surface a clear status-coded error before any
           navigation. The form never creates membership. -->
      <section class="panel glass-panel" data-testid="manual-open-panel">
        <h3>Open by slug</h3>
        <form class="form-row" @submit.prevent="openBySlug">
          <input
            v-model="manualSlug"
            type="text"
            class="text-input"
            data-testid="manual-slug-input"
            placeholder="room-slug"
            :disabled="manualOpenInFlight"
          />
          <button
            type="submit"
            class="primary-btn"
            data-testid="manual-slug-submit"
            :disabled="manualOpenInFlight || !manualSlug.trim()"
          >Open</button>
        </form>
        <p
          v-if="manualOpenError"
          class="error"
          role="alert"
          data-testid="manual-open-error"
        >
          {{ manualOpenError }}
        </p>
      </section>

      <!-- Create room. Trimmed slug + name submitted via createRoom.
           On success the form is cleared, the active-room list is
           refreshed, and the user is navigated to the new room. -->
      <section class="panel glass-panel" data-testid="create-room-panel">
        <h3>Create room</h3>
        <form class="form-stack" @submit.prevent="submitCreateRoom">
          <label class="form-label">
            Slug
            <input
              v-model="createSlug"
              type="text"
              class="text-input"
              data-testid="create-slug-input"
              placeholder="lowercase-with-dashes"
              :disabled="createInFlight"
            />
          </label>
          <label class="form-label">
            Name
            <input
              v-model="createName"
              type="text"
              class="text-input"
              data-testid="create-name-input"
              placeholder="Display name"
              :disabled="createInFlight"
            />
          </label>
          <button
            type="submit"
            class="primary-btn"
            data-testid="create-submit-btn"
            :disabled="createInFlight || !createSlug.trim() || !createName.trim()"
          >Create</button>
          <p
            v-if="createError"
            class="error"
            role="alert"
            data-testid="create-error"
          >
            {{ createError }}
          </p>
        </form>
      </section>

      <!-- Invite redemption. Treats the token as sensitive: never
           placed in the route, never persisted, never echoed in a
           toast or console. On success the input is cleared, the
           active-room list is refreshed, and the matching room's
           slug is resolved from membership.room_id. -->
      <section class="panel glass-panel" data-testid="invite-panel">
        <h3>Redeem invite token</h3>
        <form class="form-row" @submit.prevent="submitRedeemInvite">
          <input
            v-model="inviteToken"
            type="text"
            class="text-input"
            data-testid="invite-token-input"
            placeholder="paste invite token"
            autocomplete="off"
            :disabled="redeemInFlight"
          />
          <button
            type="submit"
            class="primary-btn"
            data-testid="invite-submit-btn"
            :disabled="redeemInFlight || !inviteToken.trim()"
          >Redeem</button>
        </form>
        <p
          v-if="redeemError"
          class="error"
          role="alert"
          data-testid="invite-error"
        >
          {{ redeemError }}
        </p>
        <p
          v-if="redeemSuccess"
          class="success"
          role="status"
          data-testid="invite-success"
        >
          {{ redeemSuccess }}
        </p>
      </section>
    </main>
    <ToastContainer />
  </div>
</template>

<script setup>
// R05b1: Room entry, creation, and invite redemption UI.
//
// All state is local to this view (per the sprint contract). No
// globalStore slots are introduced. Each async flow has an
// independent guard so a slow older refresh cannot overwrite a
// newer one and a double-submit cannot fire two network requests.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { globalStore } from '../store'
import { api } from '../services/api'
import { useToast } from '../composables/useToast'
import ToastContainer from '../components/ui/ToastContainer.vue'

const router = useRouter()
const toast = useToast()
const currentUser = computed(() => globalStore.currentUser)

// --- Active-room list state ---
const activeRooms = ref([])
const listInFlight = ref(false)
const listError = ref('')
// Monotonic refresh generation. A slow older refresh that lands
// after a newer refresh must NOT overwrite the newer list. The
// handler captures the generation it was started in and only
// commits on resolve if the current generation is still its own.
let listGeneration = 0

// --- Manual open state ---
const manualSlug = ref('')
const manualOpenInFlight = ref(false)
const manualOpenError = ref('')

// --- Create room state ---
const createSlug = ref('')
const createName = ref('')
const createInFlight = ref(false)
const createError = ref('')

// --- Invite redemption state ---
const inviteToken = ref('')
const redeemInFlight = ref(false)
const redeemError = ref('')
const redeemSuccess = ref('')

async function refreshActiveRooms() {
  const myGen = ++listGeneration
  listInFlight.value = true
  listError.value = ''
  try {
    const rooms = await api.listRooms('active')
    // Stale guard: a newer refresh was started before this one
    // resolved. Drop the older result without touching the list.
    if (myGen !== listGeneration) return
    activeRooms.value = Array.isArray(rooms) ? rooms : []
  } catch (e) {
    if (myGen !== listGeneration) return
    listError.value = mapListError(e)
    // Clear the list so the empty-state hint does not render stale
    // data alongside the error banner.
    activeRooms.value = []
  } finally {
    if (myGen === listGeneration) {
      listInFlight.value = false
    }
  }
}

function mapListError(e) {
  const s = e?.status
  if (s === 401) return 'You are signed out. Log in again.'
  if (s === 403) return 'You are not allowed to list rooms.'
  if (s === 400) return 'Invalid status filter.'
  return 'Could not load active rooms.'
}

function openListedRoom(room) {
  if (!room || !room.slug) return
  router.push({ name: 'Room', params: { slug: room.slug } })
}

async function openBySlug() {
  const slug = manualSlug.value.trim()
  if (!slug) return
  if (manualOpenInFlight.value) return
  manualOpenError.value = ''
  manualOpenInFlight.value = true
  try {
    const room = await api.getRoom(slug)
    if (!room || !room.slug) {
      manualOpenError.value = 'Room not found.'
      return
    }
    // Backend response carries the canonical status. Surfaces an
    // explicit hint for archived rooms (the user can still navigate;
    // RoomView will display the archived banner).
    if (room.status && room.status !== 'active') {
      manualOpenError.value = 'This room is no longer available.'
    }
    router.push({ name: 'Room', params: { slug: room.slug } })
  } catch (e) {
    manualOpenError.value = mapManualOpenError(e)
  } finally {
    manualOpenInFlight.value = false
  }
}

function mapManualOpenError(e) {
  const s = e?.status
  if (s === 400) return 'Invalid room slug.'
  if (s === 401) return 'You are signed out. Log in again.'
  if (s === 404) return 'Room not found.'
  return 'Could not open room.'
}

async function submitCreateRoom() {
  const slug = createSlug.value.trim()
  const name = createName.value.trim()
  if (!slug || !name) return
  if (createInFlight.value) return
  createError.value = ''
  createInFlight.value = true
  try {
    const room = await api.createRoom(slug, name)
    if (!room || !room.slug) {
      createError.value = 'Could not create room.'
      return
    }
    // Success path: clear the form, refresh the active list so the
    // new room shows up, then navigate.
    createSlug.value = ''
    createName.value = ''
    // Bump the list generation so the in-flight refresh (if any) is
    // dropped — the explicit refresh we kick off below is the
    // authoritative one for the post-create state.
    listGeneration += 1
    await refreshActiveRooms()
    router.push({ name: 'Room', params: { slug: room.slug } })
  } catch (e) {
    createError.value = mapCreateError(e)
  } finally {
    createInFlight.value = false
  }
}

function mapCreateError(e) {
  const s = e?.status
  if (s === 400) return 'Invalid or reserved slug or name.'
  if (s === 401) return 'You are signed out. Log in again.'
  if (s === 409) return 'A room with this slug already exists.'
  return 'Could not create room.'
}

async function submitRedeemInvite() {
  const token = inviteToken.value.trim()
  if (!token) return
  if (redeemInFlight.value) return
  redeemError.value = ''
  redeemSuccess.value = ''
  redeemInFlight.value = true
  try {
    const member = await api.redeemInvite(token)
    // The invite token is sensitive: clear the input immediately
    // after a successful response. Never place it in a URL, local
    // / session storage, console log, or user-visible toast.
    inviteToken.value = ''
    if (!member || typeof member.room_id === 'undefined') {
      redeemError.value = 'Could not redeem invite.'
      return
    }
    // Refresh the active-room list so we can resolve room_id → slug.
    // The redemption itself succeeded; a follow-up list failure
    // must NOT retry the redemption.
    listGeneration += 1
    let resolvedSlug = null
    try {
      await refreshActiveRooms()
      resolvedSlug = resolveSlugForRoomId(member.room_id)
    } catch (listErr) {
      // Surface a success message — membership was created — and
      // leave the manual Refresh button available. Do NOT echo the
      // token or any other sensitive detail.
      redeemSuccess.value = 'Membership created. Use Refresh and try opening the room by slug.'
      return
    }
    if (resolvedSlug) {
      router.push({ name: 'Room', params: { slug: resolvedSlug } })
    } else {
      // Membership succeeded but the room did not appear in the
      // active list. The user can use the manual-open form.
      redeemSuccess.value = 'Membership created. Use Refresh and try opening the room by slug.'
    }
  } catch (e) {
    redeemError.value = mapRedeemError(e)
    // The token is sensitive: do NOT include the raw token (or any
    // part of it) in user-visible error text. Clear the input on
    // failure too so a token does not linger in the form after a
    // failed redemption.
    inviteToken.value = ''
  } finally {
    redeemInFlight.value = false
  }
}

function resolveSlugForRoomId(roomId) {
  if (!activeRooms.value || !activeRooms.value.length) return null
  const match = activeRooms.value.find((r) => Number(r.id) === Number(roomId))
  return match && match.slug ? match.slug : null
}

function mapRedeemError(e) {
  const s = e?.status
  if (s === 401) return 'You are signed out. Log in again.'
  if (s === 404) return 'This invite is invalid, expired, or revoked.'
  if (s === 409) return 'This room is archived.'
  if (s === 410) return 'This invite has been used up.'
  return 'Could not redeem invite.'
}

function handleBack() {
  router.push({ name: 'Dashboard' })
}

onMounted(async () => {
  await refreshActiveRooms()
})
</script>

<style scoped>
.room-entry-view {
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

.user-name {
  font-weight: 500;
  color: var(--text-main);
}

.dashboard-btn {
  background: rgba(0, 212, 255, 0.12);
  color: var(--accent-hover);
  border: 1px solid rgba(0, 212, 255, 0.35);
  border-radius: var(--radius-sm);
  padding: 0.35rem 0.75rem;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  cursor: pointer;
  transition: background 0.2s ease, border-color 0.2s ease;
}

.dashboard-btn:hover {
  background: rgba(0, 212, 255, 0.22);
  border-color: var(--accent-hover);
}

.entry-content {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1rem;
  flex-grow: 1;
  min-height: 0;
  overflow-y: auto;
}

.panel {
  padding: 1rem 1.25rem;
  border-radius: var(--radius-md);
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}

.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem;
}

.panel h3 {
  margin: 0;
  font-size: 1rem;
}

.panel-hint {
  margin: 0;
  color: var(--text-muted);
  font-size: 0.85rem;
}

.form-row {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}

.form-stack {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}

.form-label {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  font-size: 0.8rem;
  color: var(--text-muted);
}

.text-input {
  flex: 1;
  padding: 0.45rem 0.6rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--text-muted);
  background: transparent;
  color: inherit;
}

.text-input:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.primary-btn {
  background: var(--accent);
  color: #0a0a0a;
  border: none;
  border-radius: var(--radius-sm);
  padding: 0.4rem 0.9rem;
  font-weight: 600;
  cursor: pointer;
}

.primary-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.refresh-btn {
  background: rgba(0, 212, 255, 0.12);
  color: var(--accent-hover);
  border: 1px solid rgba(0, 212, 255, 0.35);
  border-radius: var(--radius-sm);
  padding: 0.3rem 0.7rem;
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
}

.refresh-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.room-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.room-item {
  display: flex;
  gap: 0.6rem;
  align-items: center;
  padding: 0.35rem 0.5rem;
  border-radius: var(--radius-sm);
  background: rgba(0, 0, 0, 0.18);
}

.room-name {
  font-weight: 600;
  flex: 1 1 auto;
  min-width: 0;
}

.room-slug {
  font-size: 0.8rem;
  color: var(--text-muted);
  font-family: var(--font-mono, monospace);
}

.open-btn {
  background: var(--accent);
  color: #0a0a0a;
  border: none;
  border-radius: var(--radius-sm);
  padding: 0.3rem 0.7rem;
  font-weight: 600;
  cursor: pointer;
}

.error {
  color: var(--danger);
  font-size: 0.85rem;
  margin: 0;
}

.success {
  color: var(--success, #2ecc71);
  font-size: 0.85rem;
  margin: 0;
}

.empty,
.loading {
  color: var(--text-muted);
  font-style: italic;
  font-size: 0.85rem;
  margin: 0;
}

@media (max-width: 720px) {
  .entry-content {
    grid-template-columns: 1fr;
  }
}
</style>