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
        <h3>
          Queue
          <button
            class="radio-toggle-btn"
            :disabled="!canMutate || autoQueueToggleInFlight"
            :aria-pressed="!!(roomState.autoQueueConfig && roomState.autoQueueConfig.enabled)"
            @click="toggleRoomAutoQueue"
          >
            {{ roomState.autoQueueConfig && roomState.autoQueueConfig.enabled ? '📻 Radio: on' : '📻 Radio: off' }}
          </button>
        </h3>
        <p v-if="roomState.lastError" class="error" role="alert">
          {{ roomState.lastError }}
        </p>
        <ol class="queue-list">
          <li
            v-for="(song, idx) in roomState.state.songs || []"
            :key="String(song.id) + ':' + idx"
            class="queue-item"
          >
            <span class="song-title">
              {{ song.title || '(untitled)' }}
              <span v-if="song.added_by === 'system:autoqueue'" class="auto-badge" title="Added by room auto-queue">⚡ auto</span>
            </span>
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

      <!-- R11a: room chat panel. Plain-text messages between active
           room members. The history list is rendered as text only
           (never v-html) so any future sender-side HTML is
           inert. The input is disabled when the viewer is
           unauthenticated, the room is archived, the viewer was
           removed, the WS is disconnected, or a previous send is
           in flight; it is NOT disabled when the draft merely
           exceeds the code-point cap — the user must remain able
           to shorten their own draft. Send is disabled whenever
           the input is disabled OR the draft is empty/whitespace
           OR the draft exceeds the cap. -->
      <section
        v-if="!isRoomDisabled"
        class="chat-panel glass-panel"
        data-testid="chat-panel"
      >
        <h3>Chat</h3>
        <p v-if="!roomState.connected" class="hint">
          Chat is unavailable while disconnected.
        </p>
        <ol class="chat-list" data-testid="chat-list">
          <li
            v-for="(msg, idx) in chatMessages"
            :key="`${msg.id}-${idx}`"
            class="chat-message"
            data-testid="chat-message"
          >
            <span class="chat-sender">{{ msg.sender && msg.sender.display_name ? msg.sender.display_name : `user #${msg.sender ? msg.sender.user_id : '?'}` }}</span>
            <span class="chat-content">{{ msg.content }}</span>
          </li>
          <li v-if="!chatMessages.length" class="empty" data-testid="chat-empty">
            No messages yet.
          </li>
        </ol>
        <form class="chat-form" @submit.prevent="sendChat">
          <input
            v-model="chatDraft"
            type="text"
            class="chat-input"
            data-testid="chat-input"
            placeholder="Say something…"
            :disabled="!canEditChat"
          />
          <button
            type="submit"
            class="chat-send-btn"
            data-testid="chat-send"
            :disabled="!canSendChat"
          >Send</button>
        </form>
        <p
          v-if="chatDraftOverLimit"
          class="error"
          role="alert"
          data-testid="chat-over-limit"
        >
          Message is too long ({{ chatDraftCodePoints }}/{{ chatMaxCodePoints }}).
        </p>
      </section>

      <!-- R10c: room deletion + member removal host panel.
           Visible only to the host as a UI convenience (the backend
           is authoritative and surfaces 401/403/404/409 toasts).
           Hidden entirely when the room is archived or the current
           viewer was removed — both states render their own banner
           above instead. -->
      <section
        v-if="isHost && !isRoomDisabled"
        class="host-panel glass-panel"
        data-testid="host-panel"
      >
        <h3>Host controls</h3>
        <div class="host-row">
          <button
            class="archive-room-btn"
            :disabled="!canDeleteRoom"
            data-testid="archive-room-btn"
            @click="deleteRoom"
          >Archive room</button>
        </div>
        <h4>Members</h4>
        <ul v-if="(roomState.members || []).length" class="member-list" data-testid="member-list">
          <li
            v-for="m in roomState.members"
            :key="m.user_id + ':' + m.role"
            class="member-item"
          >
            <span class="member-id">user #{{ m.user_id }}</span>
            <span class="member-role" :class="`role-${m.role}`">{{ m.role }}</span>
            <button
              v-if="canRemoveMember(m)"
              class="remove-member-btn"
              :data-testid="`remove-member-${m.user_id}`"
              @click="removeRoomMember(m)"
            >Remove</button>
            <span
              v-else-if="m.role === 'host'"
              class="member-lock"
              data-testid="host-lock"
              :title="'The host cannot be removed.'"
            >(host)</span>
            <span
              v-else-if="currentUser && Number(m.user_id) === Number(currentUser.id)"
              class="member-lock"
              data-testid="self-lock"
              :title="'You cannot remove yourself; archive the room instead.'"
            >(self)</span>
          </li>
        </ul>
        <p v-else class="empty">No members loaded yet.</p>
      </section>

      <!-- R10c: archived / removed banners. Rendered instead of the
           normal room surface so the user gets a clear, unambiguous
           disabled state. The connection may still be open on the
           server side (room_archived does not force-close per-room
           WS); the local view is the authoritative UX. -->
      <section
        v-if="roomArchived"
        class="archived-banner glass-panel"
        role="status"
        data-testid="archived-banner"
      >
        <h3>This room has been archived.</h3>
        <p>No further queue, playback, or membership changes can be made. Navigate back to the dashboard to choose another room.</p>
      </section>
      <section
        v-if="roomRemoved"
        class="removed-banner glass-panel"
        role="status"
        data-testid="removed-banner"
      >
        <h3>You have been removed from this room.</h3>
        <p>The host removed you. Room controls are disabled. Navigate back to the dashboard.</p>
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
    return { state: { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] }, lastError: null, connected: false, recoveryInFlight: false, archived: false, removed: false, members: [] }
  }
  return globalStore.roomQueues[slug.value]
})
const canMutate = computed(() => !!currentUser.value && roomState.value.connected && !roomState.value.archived && !roomState.value.removed)
const canClear = computed(() => !!currentUser.value && roomState.value.connected && !roomState.value.archived && !roomState.value.removed)
const addUrl = ref('')
// R11a: chat draft + send-in-flight guard. The send button is
// disabled while chatSendInFlight is true so a user cannot fire
// two POSTs before the first completes (the response would
// append the persisted row AND the next /chat/messages refresh
// would surface it again — harmless, but the local view would
// briefly double-render before the cache de-dupes on seq).
const chatDraft = ref('')
const chatSendInFlight = ref(false)
// R11a (corrective pass): the chat-history seed has two pieces of
// per-connection state:
//   - chatHistoryFetched: true once a GET /chat/messages has
//     RESOLVED SUCCESSFULLY for this connection. It suppresses
//     further seeds from later room_queue_sync events.
//   - chatHistoryFetchInFlight: true while a GET is in-flight.
//     It prevents a second sync from triggering a duplicate
//     network request while we wait for the first one.
// On failure we keep chatHistoryFetched=false so a later
// room_queue_sync is allowed to retry; the in-flight flag is cleared
// unconditionally in a finally so a future sync is never permanently
// gated.
//
// R11a (deferred lifecycle follow-up): both flags are scoped to a
// numeric generation token (`chatGeneration`). Each new mount and
// each slug change bumps the token. A pending GET records the
// generation it was started in; on resolve it only flips
// `chatHistoryFetched` if its generation still matches the current
// token. This guarantees a slow GET that was started for an OLD
// connection (the user already navigated away) can never mutate the
// NEW connection's fetched / in-flight flags.
let chatHistoryFetched = false
let chatHistoryFetchInFlight = false
let chatGeneration = 0
let wsClient = null
let suppressSeqGap = false
// R11a (deferred lifecycle follow-up): `suppressSeqGap` and its
// 50ms release timer are also scoped to a numeric generation. The
// setTimeout closure captures the generation it was queued in; if
// the current generation has advanced by the time the timer fires,
// the closure short-circuits without touching the flag. A
// teardown / slug change bumps the generation so any stale timer
// from a previous connection cannot re-enable gap detection for
// the new one.
let seqGapGeneration = 0
let suppressSeqGapTimer = null
// Track the slug we actually connected to so unmount can clean it up
// even if the route reactive value drifts (test mounts without an initial slug).
let connectedSlug = null

// R11a: chatMessages exposes the per-room cache as a reactive
// computed. The cap is owned by the store mutator
// (applyRoomChatMessageCreated), so this computed only mirrors
// the slice. The empty-state branch renders "No messages yet."
// when the slice is empty.
const chatMessages = computed(() => {
  const s = globalStore.roomQueues[slug.value]
  return (s && Array.isArray(s.messages)) ? s.messages : []
})

// R11a (corrective pass): the send button is gated by two concepts:
//   - canEditChat: the viewer is allowed to interact with the
//     input (auth + connected + active member + not archived/
//     removed + no send in flight). The input is NOT disabled just
//     because the draft is over the code-point cap — the user must
//     remain able to shorten their draft.
//   - canSendChat: canEditChat + non-empty trimmed draft + <=500
//     code points. Drives the Send button's disabled binding.
// Splitting these two is required because the previous `canChat`
// covered both and that locked the input when the draft exceeded
// the cap, blocking the user from editing down.
const canEditChat = computed(
  () => !!currentUser.value
    && roomState.value.connected
    && !roomState.value.archived
    && !roomState.value.removed
    && !chatSendInFlight.value
)
const canSendChat = computed(
  () => canEditChat.value
    && !chatDraftOverLimit.value
    && !!chatDraft.value.trim()
)
// Backwards-compatible alias for tests that pin `vm.canChat`.
const canChat = canSendChat

// R11a: chat code-point cap. The native maxlength attribute is
// intentionally NOT applied (it counts UTF-16 code units, which
// under-counts BMP code points and over-counts surrogate pairs).
// Array.from(str) splits a string into its Unicode code points
// so the resulting length matches the user-perceived character
// count and pairs with the utf8.RuneCountInString server-side cap.
// Server validation is still authoritative; this is a UI affordance
// only.
const chatMaxCodePoints = 500
const chatDraftCodePoints = computed(() => Array.from(chatDraft.value || '').length)
const chatDraftOverLimit = computed(() => chatDraftCodePoints.value > chatMaxCodePoints)

function applyMessage(msg) {
  switch (msg.type) {
    case 'room_queue_sync':
      globalStore.setRoomQueueState(slug.value, msg.data?.state || { songs: [], current_index: -1, current_song: null, status: 'stopped', queue: [], history: [] })
      // R11a (corrective pass): the initial room_queue_sync event
      // is the per-room hub's confirmation that the client is
      // registered, so it is the safe point to start the
      // chat-history GET. Running the GET BEFORE wsClient.connect()
      // creates a permanent race where a message sent in the
      // window between the GET and the WS registration is
      // missed. Two flags gate the seed:
      //   - chatHistoryFetched: false until a GET has resolved
      //     successfully. Set to true only AFTER the seeded
      //     messages are merged into the store. A failed GET
      //     leaves this false so a later room_queue_sync may
      //     retry.
      //   - chatHistoryFetchInFlight: true while a GET is
      //     pending. Prevents a duplicate network call from a
      //     second sync arriving while the first is still in
      //     flight.
      // Both flags are reset on teardown and on slug change so
      // the next room re-seeds with the same retry semantics.
      if (!chatHistoryFetched && !chatHistoryFetchInFlight) {
        seedRoomChatMessagesFromRest(slug.value)
      }
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
    // R09g: room auto-queue events. room_auto_queue_added carries
    // the post-mutation snapshot (payload.state preferred); the
    // fallback path mirrors the additive R09f payload fields.
    // room_auto_queue_config_changed only updates the per-room
    // autoQueueConfig slice — it MUST NOT touch globalStore.autoQueueConfig.
    case 'room_auto_queue_added':
      globalStore.applyRoomAutoQueueAdded(slug.value, msg.data)
      break
    case 'room_auto_queue_config_changed':
      globalStore.applyRoomAutoQueueConfigChanged(slug.value, msg.data)
      break
    // R10c: room deletion + member removal events. The per-room
    // hub broadcasts room_archived (with reason host_archived) to
    // every client on the active→archived transition; we flip the
    // archived flag and let the local view render a disabled surface.
    // room_member_removed is targeted at the removed user before the
    // server closes the WS with code 1008; we flip the removed flag
    // so the view renders a clear "removed from room" message and
    // disables controls. room_members_changed is broadcast to the
    // remaining clients and updates the cached member list used by
    // the remove-member control.
    case 'room_archived':
      globalStore.markRoomArchived(slug.value)
      break
    case 'room_member_removed':
      // Targeted event: if this client IS the removed user, surface
      // a disabled/removed view. We rely on payload.user_id matching
      // the current user id; the server only delivers this envelope
      // to the affected connection so any delivery is authoritative.
      {
        const removedID = typeof msg.data?.user_id === 'number'
          ? msg.data.user_id
          : Number(msg.data?.user_id)
        const me = currentUser.value?.id
        if (me != null && Number(me) === removedID) {
          globalStore.markRoomRemovedAsCurrentUser(slug.value)
          toast.info('You have been removed from this room.')
        }
      }
      break
    case 'room_members_changed':
      globalStore.applyRoomMembersChanged(slug.value, msg.data)
      break
    // R11a: room chat. Append the post-mutation message to the
    // room-local cache via the merge-aware store mutator. The
    // mutator caps the cache at MaxRoomChatMessages, sorts oldest
    // → newest by created_at (with id as the deterministic
    // tie-breaker), and dedupes by id so a POST response and a
    // subsequent room_chat_message_created WS event for the same
    // id collapse to one row. The cache is seeded by the
    // post-WS-registration REST GET that fires from the first
    // room_queue_sync event.
    case 'room_chat_message_created':
      globalStore.applyRoomChatMessageCreated(slug.value, msg.data)
      break
    default:
      // Ignore global / unrelated event types per the R07c contract.
      break
  }
}

async function onGap() {
  if (suppressSeqGap) return
  const targetSlug = slug.value
  if (!targetSlug) return
  // R11a (final lifecycle correction): use the IfExists variant
  // so a stale onGap that lands AFTER the slug watcher has cleared
  // the old room entry (or AFTER a teardown) does NOT recreate
  // the entry with a default `recoveryInFlight = true`. The
  // standard mutator calls _ensureRoomEntry, which would silently
  // create a phantom old-room entry on a stale call.
  globalStore.setRoomQueueRecoveryInFlightIfExists(targetSlug, true)
  // R11a (corrective pass): queue recovery and chat history
  // recovery are TWO INDEPENDENT operations on every accepted
  // onGap. They each succeed or fail on their own; one failure
  // MUST NOT skip the other. The existing queue error/toast
  // behavior is preserved.
  // R11a (deferred lifecycle follow-up): capture the seq-gap
  // generation this recovery was started in so the queued
  // setTimeout can short-circuit if a teardown / slug change has
  // advanced the generation by the time the timer fires.
  const myGapGeneration = seqGapGeneration
  const queuePromise = (async () => {
    try {
      const fresh = await api.getRoomQueue(targetSlug)
      // Stale guard: either the connection was torn down or the
      // user navigated to a different slug in the meantime.
      if (myGapGeneration !== seqGapGeneration) return { ok: false, stale: true }
      if (targetSlug !== slug.value) return { ok: false, stale: true }
      // Use the write-if-exists variants so a stale resolution
      // does NOT recreate a cleared old-room store entry.
      globalStore.setRoomQueueStateIfExists(targetSlug, fresh)
      globalStore.setRoomQueueSeq(targetSlug, 0)
      suppressSeqGap = true
      // Cancel any prior release timer from this generation
      // before queueing a fresh one.
      if (suppressSeqGapTimer) clearTimeout(suppressSeqGapTimer)
      suppressSeqGapTimer = setTimeout(() => {
        // The timer only resets the flag if the generation is
        // still ours. A teardown / slug change will have bumped
        // seqGapGeneration, cleared the timer, and reset the
        // flag — so the stale callback is a no-op.
        if (myGapGeneration === seqGapGeneration) {
          suppressSeqGap = false
        }
        suppressSeqGapTimer = null
      }, 50)
      return { ok: true }
    } catch (e) {
      if (myGapGeneration !== seqGapGeneration) return { ok: false, stale: true }
      // Stale-slug guard — a recovery GET that resolves after a
      // slug change must not surface toasts or write to a stale
      // entry.
      if (targetSlug !== slug.value) return { ok: false, stale: true }
      const status = e?.status
      const reason = e?.message || ''
      const message = status === 401 ? 'You are signed out. Log in again.'
        : status === 403 ? `You are not a member of this room (${reason || 'forbidden'}).`
        : status === 404 ? 'Room not found.'
        : status === 409 ? 'Room is archived or in conflict.'
        : 'Could not resync room queue.'
      globalStore.setRoomQueueErrorIfExists(targetSlug, message)
      toast.error(message)
      return { ok: false, stale: false }
    }
  })()
  const chatPromise = (async () => {
    try {
      const chat = await api.getRoomChatMessages(targetSlug, 50)
      if (myGapGeneration !== seqGapGeneration) return { ok: false, stale: true }
      if (targetSlug !== slug.value) return { ok: false, stale: true }
      globalStore.setRoomChatMessagesIfExists(targetSlug, chat.messages || [])
      return { ok: true }
    } catch (chatErr) {
      // The chat recovery is best-effort. The prior chat
      // messages remain in the store. A toast is intentionally
      // NOT raised here — the queue recovery already toasts on
      // failure and a per-recovery chat toast would be noise.
      // eslint-disable-next-line no-console
      console.warn('room chat history recovery failed', chatErr)
      return { ok: false, stale: targetSlug !== slug.value || myGapGeneration !== seqGapGeneration }
    }
  })()
  try {
    await Promise.allSettled([queuePromise, chatPromise])
  } finally {
    // R11a (final lifecycle correction): onGap's finally only
    // finalizes the recovery flag when both invariants hold:
    //   1. the connection's generation token is still current
    //      (no teardown or slug change happened during the
    //      recovery)
    //   2. the targetSlug still matches the active slug (the
    //      user did not navigate away)
    // A stale recovery that satisfies neither invariant MUST
    // NOT:
    //   - recreate the cleared old room entry (the standard
    //     mutator's _ensureRoomEntry would do that), nor
    //   - clear / modify the NEW room's recovery state, nor
    //   - show a stale toast (toasts are gated inside the
    //     individual IIFEs on the same invariants).
    // The IfExists variant is the entry-mutating primitive and
    // is a no-op when the slug's entry was already cleared.
    if (myGapGeneration === seqGapGeneration && targetSlug === slug.value) {
      globalStore.setRoomQueueRecoveryInFlightIfExists(targetSlug, false)
    }
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

// R09g: seed the per-room auto-queue config from the existing R09f
// status endpoint. This is the initial paint for the Radio toggle so
// the UI is consistent with the room's persisted setting even if the
// WS room_auto_queue_config_changed event is delayed. 401/403/404/409
// surface as a clear toast but do NOT block the rest of the room
// from rendering — the toggle just stays at its seed value.
async function seedRoomAutoQueueConfigFromRest() {
  try {
    const cfg = await api.getRoomAutoQueueStatus(slug.value)
    globalStore.setRoomAutoQueueConfig(slug.value, cfg.enabled, cfg.strategy)
  } catch (e) {
    const status = e?.status
    if (status === 401) toast.error('You are signed out. Log in again.')
    else if (status === 403) toast.error('You are not a member of this room.')
    else if (status === 404) toast.error('Room not found.')
    else if (status === 409) toast.error('Room is archived or in conflict.')
  }
}

// R10c: seed the per-room member list from the GET /members read
// surface. room_members_changed is the authoritative delta after a
// removal, but it does not fire BEFORE the first removal — so the
// host remove-member UI must have an authoritative initial source.
// 401/403/404/409 surface as a clear toast but do NOT block the rest
// of the room from rendering; the panel just renders the seeded
// "no members loaded yet" empty state. Archived rooms map to 409
// (read-only authoritative surface; the host panel is hidden anyway).
async function seedRoomMembersFromRest() {
  try {
    const res = await api.getRoomMembers(slug.value)
    globalStore.applyRoomMembersChanged(slug.value, res)
  } catch (e) {
    const status = e?.status
    if (status === 401) toast.error('You are signed out. Log in again.')
    else if (status === 403) toast.error('You are not a member of this room.')
    else if (status === 404) toast.error('Room not found.')
    else if (status === 409) toast.error('Room is archived or in conflict.')
  }
}

// R11a (corrective pass): seed the room chat history from
// GET /chat/messages. The history is bounded server-side
// (default 50, max 100) and ordered oldest → newest. The function
// accepts an explicit targetSlug so the caller can pin the history
// to the slug it was invoked for, even if the route's reactive
// slug has drifted in the meantime. The two retry-semantics flags
// are managed here so all callers (applyMessage on sync, recovery
// on gap) get the same retry / no-double-fetch behavior:
//   - chatHistoryFetchInFlight is set true BEFORE the await and
//     cleared in a finally so a second sync that arrives during
//     the await NEVER triggers a second network request.
//   - chatHistoryFetched is set true ONLY after the messages are
//     merged into the store. A failed GET leaves it false so the
//     next sync may retry.
// The function returns void; it is fire-and-forget from
// applyMessage. 401/403/404/409 surface as a clear toast for the
// currently-viewed room only; stale GETs (the user navigated away)
// must not produce a toast.
async function seedRoomChatMessagesFromRest(targetSlug) {
  if (!targetSlug) return
  if (chatHistoryFetched) return
  if (chatHistoryFetchInFlight) return
  // R11a (deferred lifecycle follow-up): record the generation
  // this GET was started in so the resolve / reject paths can
  // short-circuit if a teardown or slug change has advanced the
  // generation by the time the network call returns.
  const myGeneration = chatGeneration
  chatHistoryFetchInFlight = true
  try {
    const res = await api.getRoomChatMessages(targetSlug, 50)
    // Stale guard: either the connection has been torn down or
    // the user navigated to a different slug in the meantime.
    if (myGeneration !== chatGeneration) return
    if (targetSlug !== slug.value) return
    globalStore.setRoomChatMessages(targetSlug, res.messages || [])
    chatHistoryFetched = true
  } catch (e) {
    if (myGeneration !== chatGeneration) return
    // Only surface toasts for the currently-viewed room; a stale
    // GET for a slug the user already left must not pollute the
    // current room's toast stream.
    if (targetSlug === slug.value) {
      const status = e?.status
      if (status === 401) toast.error('You are signed out. Log in again.')
      else if (status === 403) toast.error('You are not a member of this room.')
      else if (status === 404) toast.error('Room not found.')
      else if (status === 409) toast.error('Room is archived or in conflict.')
      else if (status === 400) toast.error('Invalid chat request.')
    }
    // chatHistoryFetched stays false so a later room_queue_sync
    // is allowed to retry.
  } finally {
    // Only clear the in-flight flag if the in-flight GET still
    // belongs to the current generation. A new mount / slug
    // change already cleared / will reset the flag itself.
    if (myGeneration === chatGeneration) {
      chatHistoryFetchInFlight = false
    }
  }
}

function buildRoomClient(targetSlug) {
  // R10c: a 1008 close means the host removed this client. We
  // surface a disabled/removed view and do NOT silently reconnect
  // — the backend will reject subsequent /ws/rooms/{slug} connects
  // because the membership row is gone. The onClose path is also
  // used for benign disconnects, so the rule is:
  //   - close code 1008 (policy violation) → mark the current
  //     viewer removed unless the room is already archived
  //     (the R10b broadcast order sends room_archived first; a
  //     1008 without an archived state means the removal was
  //     targeted at the current viewer, not a global archive).
  //   - any other close (1000 normal, 1006 abnormal, etc.) is a
  //     benign disconnect — flip connected=false only and let the
  //     user navigate. No silent reconnect.
  const onClose = (event) => {
    globalStore.setRoomQueueConnected(targetSlug, false)
    const code = event && typeof event.code === 'number' ? event.code : null
    const entry = globalStore.roomQueues[targetSlug]
    const alreadyArchived = !!(entry && entry.archived)
    if (code === 1008 && !alreadyArchived) {
      globalStore.markRoomRemovedAsCurrentUser(targetSlug)
      toast.info('You have been removed from this room.')
    }
  }
  const options = {
    onMessage: applyMessage,
    onGap,
    onOpen: () => globalStore.setRoomQueueConnected(targetSlug, true),
    onClose,
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
  // R11a (corrective pass): the chat seed flags are per-WS-client;
  // a teardown must reset BOTH so the next mount / slug change can
  // re-seed the next room's history after its first sync and any
  // half-finished GET does not leave the next room stuck.
  chatHistoryFetched = false
  chatHistoryFetchInFlight = false
  // R11a (deferred lifecycle follow-up): bump the chat
  // generation so any in-flight GET from this connection can no
  // longer mutate the flags (it will see myGeneration !==
  // chatGeneration in its finally block and bail).
  chatGeneration += 1
  // Cancel any pending suppressSeqGap release timer from this
  // connection and bump the seq-gap generation so a stale timer
  // callback cannot re-enable gap detection for the next one.
  if (suppressSeqGapTimer) {
    clearTimeout(suppressSeqGapTimer)
    suppressSeqGapTimer = null
  }
  seqGapGeneration += 1
  suppressSeqGap = false
}

onMounted(async () => {
  const target = slug.value
  if (target == null) return
  connectedSlug = target
  // R11a (corrective pass): chat history is NOT seeded before the
  // WS client connects. Running the GET in this window creates a
  // permanent race where a message sent between the GET and the
  // WS registration is missed. The seed is fired by the first
  // room_queue_sync event (see applyMessage) after the per-room
  // hub confirms the client is registered.
  chatHistoryFetched = false
  chatHistoryFetchInFlight = false
  globalStore.setRoomQueueConnected(target, false)
  await seedStateFromRest()
  await seedRoomAutoQueueConfigFromRest()
  await seedRoomMembersFromRest()
  wsClient = buildRoomClient(target)
  wsClient.connect()
})

watch(slug, async (newSlug) => {
  if (newSlug == null || newSlug === connectedSlug) return
  teardownCurrentClient()
  connectedSlug = newSlug
  // R11a (corrective pass): reset the per-connection chat seed
  // flags so the new room re-seeds exactly once after the first
  // room_queue_sync event. The flags are intentionally
  // per-mount, not per-slug, so the seed is tied to the WS client
  // lifetime. chatHistoryFetchInFlight is reset so a stuck
  // half-finished GET against the OLD slug cannot permanently
  // gate the NEW room from seeding.
  chatHistoryFetched = false
  chatHistoryFetchInFlight = false
  globalStore.setRoomQueueConnected(newSlug, false)
  // Mirror the mount path: REST-seed the new room's queue, its
  // auto-queue config, AND its member list before opening the
  // room WebSocket so the UI is consistent with the next room's
  // state even if the WS initial sync is delayed. The chat
  // history GET is intentionally deferred until after the WS
  // initial sync (see applyMessage / room_queue_sync handler).
  await seedStateFromRest()
  await seedRoomAutoQueueConfigFromRest()
  await seedRoomMembersFromRest()
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

// R11a: send a plain-text chat message. The send-in-flight guard
// prevents a double-POST if the user double-clicks before the
// first response lands. On success the input is cleared. The
// POST response body is { message: { ... } } (same shape as the
// room_chat_message_created WS data payload); the response is
// applied to the local cache immediately via the merge-aware
// store mutator so the sender sees the persisted row even if the
// WS fan-out never lands (e.g. the connection drops between POST
// success and the hub broadcast). The store merge dedupes by id,
// so a later room_chat_message_created WS event for the same id
// collapses to one row.
async function sendChat() {
  if (!canSendChat.value) return
  const trimmed = chatDraft.value.trim()
  if (!trimmed) return
  chatSendInFlight.value = true
  const targetSlug = slug.value
  try {
    const body = await api.sendRoomChatMessage(targetSlug, trimmed)
    if (body && body.message && targetSlug === slug.value) {
      globalStore.applyRoomChatMessageFromPost(targetSlug, body)
    }
    chatDraft.value = ''
  } catch (e) {
    const s = e?.status
    if (s === 400) toast.error('Message is empty or too long.')
    else if (s === 401) toast.error('You are signed out. Log in again.')
    else if (s === 403) toast.error('You are not a member of this room.')
    else if (s === 404) toast.error('Room not found.')
    else if (s === 409) toast.error('Room is archived or in conflict.')
    else toast.error('Could not send message.')
  } finally {
    chatSendInFlight.value = false
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

// --- R10c host-gating + archived/removed computed values ---
//
// These are UI conveniences only. The backend remains authoritative
// on authorization — the frontend does NOT pre-check the role beyond
// hiding the destructive controls. currentUser.role is the same field
// DashboardView and QueueList.vue already consult.
const isHost = computed(() => currentUser.value?.role === 'host')
// roomArchived / roomRemoved flip true on the matching R10b
// WebSocket envelope (see applyMessage) and persist for the rest of
// the lifetime of the route. Once flipped, every destructive control
// in the view short-circuits via the read-only state below.
const roomArchived = computed(() => !!roomState.value?.archived)
const roomRemoved = computed(() => !!roomState.value?.removed)
const isRoomDisabled = computed(() => roomArchived.value || roomRemoved.value)
// canDeleteRoom mirrors canMutate + isHost + not already disabled +
// an in-flight guard for double-click safety. The backend is the
// authoritative gate.
const deleteRoomInFlight = ref(false)
const canDeleteRoom = computed(() => isHost.value && canMutate.value && !isRoomDisabled.value && !deleteRoomInFlight.value)
// removeMemberInFlight guards the per-target delete button so a
// double-click cannot fire two requests on the same target.
const removeMemberInFlight = ref(new Set())
function canRemoveMember(member) {
  if (!isHost.value || !canMutate.value || isRoomDisabled.value) return false
  if (!member || typeof member.user_id !== 'number') return false
  if (member.role === 'host') return false
  if (currentUser.value != null && Number(member.user_id) === Number(currentUser.value.id)) return false
  if (removeMemberInFlight.value.has(member.user_id)) return false
  return true
}

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

// R09g: room auto-queue toggle. The backend enforces host/admin;
// the frontend does not pre-check the role. The toggle button is
// disabled when the room is disconnected, the user is signed out, or
// a previous toggle is in flight (so a double-click cannot fire two
// requests). The post-response store apply uses the response body;
// the room_auto_queue_config_changed WS event that follows is
// authoritative and a no-op for an already-correct local state.
const autoQueueToggleInFlight = ref(false)
async function toggleRoomAutoQueue() {
  if (autoQueueToggleInFlight.value) return
  if (!canMutate.value) return
  const current = !!roomState.value?.autoQueueConfig?.enabled
  const next = !current
  autoQueueToggleInFlight.value = true
  try {
    const cfg = await api.setRoomAutoQueueEnabled(slug.value, next)
    if (cfg) {
      globalStore.setRoomAutoQueueConfig(slug.value, cfg.enabled, cfg.strategy)
    }
  } catch (e) {
    const s = e?.status
    if (s === 401) toast.error('You are signed out. Log in again.')
    else if (s === 403) toast.error('Only the host or an admin can toggle room auto-queue.')
    else if (s === 404) toast.error('Room not found.')
    else if (s === 409) toast.error('Room is archived or in conflict.')
    else toast.error('Could not toggle room auto-queue.')
  } finally {
    autoQueueToggleInFlight.value = false
  }
}

function handleBack() {
  router.push({ name: 'Dashboard' })
}

// --- R10c: delete-room + remove-member handlers ---
//
// Both handlers gate on the local view (isHost + connected +
// not-archived/removed) for UX only; the backend is authoritative
// and surfaces 400/401/403/404/409 toasts. window.confirm is the
// lightest confirmation surface that matches the R10a contract
// (no body confirmation). On success the local store flips to the
// archived/removed view immediately so the user sees the result
// without waiting for the WS envelope (the envelope will be a
// no-op for the correct state).

async function deleteRoom() {
  if (!canDeleteRoom.value) return
  // Confirmation: native confirm() — matches the R10a contract
  // (no body confirmation; the host role is the gate).
  const confirmed = window.confirm(`Archive room "${slug.value}"? This soft-archives the room. Members can no longer mutate or play back.`)
  if (!confirmed) return
  deleteRoomInFlight.value = true
  try {
    await api.deleteRoom(slug.value)
    // Optimistically flip the local view; the room_archived
    // envelope will arrive on /ws/rooms/{slug} and is a no-op
    // for an already-archived state.
    globalStore.markRoomArchived(slug.value)
    toast.success(`Room "${slug.value}" archived.`)
  } catch (e) {
    const s = e?.status
    if (s === 400) toast.error('Invalid room slug.')
    else if (s === 401) toast.error('You are signed out. Log in again.')
    else if (s === 403) toast.error('Only the host can archive this room.')
    else if (s === 404) toast.error('Room not found.')
    else if (s === 409) toast.error('Room is archived or in conflict.')
    else toast.error('Could not archive the room.')
  } finally {
    deleteRoomInFlight.value = false
  }
}

async function removeRoomMember(member) {
  if (!canRemoveMember(member)) return
  const display = member?.user_id
  const confirmed = window.confirm(`Remove member ${display} from room "${slug.value}"? They will be disconnected immediately.`)
  if (!confirmed) return
  removeMemberInFlight.value.add(member.user_id)
  removeMemberInFlight.value = new Set(removeMemberInFlight.value) // trigger reactivity
  try {
    await api.removeRoomMember(slug.value, member.user_id)
    toast.success(`Member ${display} removed.`)
    // The room_members_changed envelope will refresh the cached
    // list; we do not locally mutate members here so the cache
    // remains the WS-authoritative view.
  } catch (e) {
    const s = e?.status
    if (s === 400) toast.error('Cannot remove that member (host cannot remove self or the host).')
    else if (s === 401) toast.error('You are signed out. Log in again.')
    else if (s === 403) toast.error('Only the host can remove members.')
    else if (s === 404) toast.error('Member not found in this room.')
    else if (s === 409) toast.error('Room is archived or in conflict.')
    else toast.error('Could not remove member.')
  } finally {
    const next = new Set(removeMemberInFlight.value)
    next.delete(member.user_id)
    removeMemberInFlight.value = next
  }
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

/* R09g: small room-scoped radio toggle that sits inline with the
   Queue heading. Disabled when disconnected, unauthenticated, or a
   toggle is in flight. */
.radio-toggle-btn {
  margin-left: 0.5rem;
  background: var(--accent);
  color: #0a0a0a;
  border: none;
  border-radius: var(--radius-sm);
  padding: 0.2rem 0.6rem;
  font-weight: 600;
  cursor: pointer;
  font-size: 0.85rem;
}
.radio-toggle-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.auto-badge {
  margin-left: 0.4rem;
  font-size: 0.7rem;
  color: var(--accent);
  font-weight: 600;
}

/* R10c: host-only delete-room + remove-member panel. Visually
   distinct (separate glass panel) so the destructive affordance
   is not confused with the queue/playback controls above. */
.host-panel {
  grid-column: 1 / -1;
  padding: 1rem;
  border-radius: var(--radius-md);
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

/* R11a: chat panel. Mirrors the host-panel layout primitive so
   the panel stretches full-width and matches the rest of the
   room surface. The list is rendered as plain text via {{ }}
   (never v-html) so any future sender-side HTML is inert. */
.chat-panel {
  grid-column: 1 / -1;
  padding: 1rem;
  border-radius: var(--radius-md);
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.chat-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  max-height: 18rem;
  overflow-y: auto;
}
.chat-message {
  font-size: 0.95rem;
  line-height: 1.3;
  word-break: break-word;
  white-space: pre-wrap;
}
.chat-sender {
  font-weight: 600;
  margin-right: 0.4rem;
}
.chat-form {
  display: flex;
  gap: 0.5rem;
  margin-top: 0.4rem;
}
.chat-input {
  flex: 1;
  padding: 0.4rem 0.6rem;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.15));
  background: rgba(0, 0, 0, 0.2);
  color: inherit;
}
.chat-input:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
.chat-send-btn {
  padding: 0.4rem 0.9rem;
  border-radius: var(--radius-sm);
  border: none;
  background: var(--accent, #4a90e2);
  color: #fff;
  font-weight: 600;
  cursor: pointer;
}
.chat-send-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.host-row {
  display: flex;
  gap: 0.5rem;
}
.archive-room-btn {
  background: var(--danger, #c0392b);
  color: #fff;
  border: none;
  border-radius: var(--radius-sm);
  padding: 0.4rem 0.9rem;
  font-weight: 600;
  cursor: pointer;
}
.archive-room-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.member-list {
  list-style: none;
  padding: 0;
  margin: 0;
}
.member-item {
  display: flex;
  gap: 0.75rem;
  align-items: center;
  padding: 0.25rem 0;
}
.member-id {
  font-weight: 500;
  min-width: 7rem;
}
.member-role {
  font-size: 0.75rem;
  padding: 0.1rem 0.4rem;
  border-radius: var(--radius-sm);
  background: var(--accent);
  color: #0a0a0a;
}
.member-role.role-admin { background: #8e44ad; color: #fff; }
.member-role.role-guest { background: #555; color: #fff; }
.remove-member-btn {
  background: var(--danger, #c0392b);
  color: #fff;
  border: none;
  border-radius: var(--radius-sm);
  padding: 0.25rem 0.6rem;
  font-size: 0.85rem;
  font-weight: 600;
  cursor: pointer;
}
.member-lock {
  font-size: 0.75rem;
  color: var(--text-muted);
  font-style: italic;
}

/* R10c: disabled-state banners. Same surface as the other panels
   but with a left accent stripe so the eye lands on them
   immediately on first paint. */
.archived-banner,
.removed-banner {
  grid-column: 1 / -1;
  padding: 1rem;
  border-radius: var(--radius-md);
  border-left: 4px solid var(--accent);
}
.archived-banner h3,
.removed-banner h3 {
  margin-top: 0;
}
.archived-banner {
  border-left-color: var(--text-muted);
}
.removed-banner {
  border-left-color: var(--danger, #c0392b);
}
</style>
