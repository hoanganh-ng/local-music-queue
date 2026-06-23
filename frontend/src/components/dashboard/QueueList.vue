<template>
  <div class="queue-list glass-panel">
    <div class="queue-header">
      <div class="queue-header__title-row">
        <div class="queue-header__title-group">
          <h3 class="queue-header__title cyber-glitch">Up Next</h3>
          <span class="queue-count" :aria-label="`Up next queue length: ${queue.length}`">{{ queue.length }} songs</span>
        </div>
        <span
          v-if="currentUser"
          class="priority-indicator"
          :aria-label="`${currentUser.priority_balance} priority tokens`"
        >
          <span class="priority-indicator__label">Priority tokens</span>
          <span class="priority-indicator__value">
            <span aria-hidden="true">⚡</span> {{ currentUser.priority_balance }}
          </span>
        </span>
      </div>

      <div v-if="canControl && queue.length > 0" class="queue-header__actions">
        <button class="clear-btn" @click="handleClear">Clear</button>
      </div>
    </div>

    <div class="queue-items" v-if="queue.length > 0">
      <TransitionGroup name="list">
        <div
          v-for="(song, index) in queue"
          :key="song.id || index"
          class="queue-item"
          :class="{ 'prioritized': song.is_prioritized }"
        >
          <!-- Top Row: Number + Badges + Title + Remove -->
          <div class="item-top-row">
            <div class="item-number">{{ index + 1 }}</div>

            <div class="item-badges" aria-hidden="true">
              <span v-if="song.is_prioritized" class="priority-icon" title="Prioritized">⚡</span>
              <span v-if="song.added_by === 'system:autoqueue'" class="auto-badge" title="Added by radio mode">📻</span>
            </div>

            <div class="item-title">{{ song.title || song.url }}</div>

            <button
              v-if="canRemoveSong(song)"
              class="remove-btn"
              @click="handleRemove(index)"
              :title="`Remove ${song.title || song.url || 'song'} from queue`"
              :aria-label="`Remove ${song.title || song.url || 'song'} from queue`"
            >
              &times;
            </button>
          </div>

          <!-- Second Row: Metadata -->
          <div class="item-meta-row">
            <span class="item-meta">Added by {{ song.added_by }}</span>
            <span v-if="song.duration" class="item-meta-sep" aria-hidden="true">·</span>
            <span v-if="song.duration" class="item-meta">{{ formatDuration(song.duration) }}</span>
          </div>

          <!-- Third Row: Action Buttons -->
          <div class="item-actions" v-if="shouldShowPrioritizeButton(song) || (currentUser && currentUser.role !== 'host')">
            <button
              v-if="shouldShowPrioritizeButton(song)"
              class="prioritize-btn"
              :class="{ 'loading': prioritizingIndex === index }"
              :disabled="!canPrioritize(song) || prioritizingIndex === index"
              :title="getPrioritizeTooltip(song)"
              @click="handlePrioritize(index)"
            >
              <span v-if="prioritizingIndex === index">⏳</span>
              <span v-else>⚡</span>
              {{ prioritizingIndex === index ? 'Bumping...' : 'Bump' }}
            </button>

            <VoteButton
              v-if="currentUser && currentUser.role !== 'host'"
              voteType="prioritize"
              :songID="song.id"
              :songIndex="queueIndexFor(index)"
              :disabled="currentUser.role === 'host'"
              compact
            />
          </div>
        </div>
      </TransitionGroup>
    </div>

    <div v-else class="empty-queue">
      Queue is empty.
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../../services/api'
import { globalStore } from '../../store'
import { sessionHelper } from '../../services/session'
import VoteButton from './VoteButton.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const props = defineProps({
  queue: {
    type: Array,
    default: () => []
  },
  currentIndex: {
    type: Number,
    default: -1
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

const currentUser = computed(() => globalStore.currentUser)
const prioritizingIndex = ref(null)
const toast = useToast()
const { confirm } = useConfirm()
const router = useRouter()

function canRemoveSong(song) {
  if (!currentUser.value) return false
  const role = currentUser.value.role
  if (role === 'host' || role === 'admin') return true
  return song.added_by_id !== undefined && song.added_by_id !== 0 && song.added_by_id === currentUser.value.id
}

function shouldShowPrioritizeButton(song) {
  if (!currentUser.value) return false
  return song.added_by_id === currentUser.value.id
}

function canPrioritize(song) {
  if (!currentUser.value) return false

  // User must own the song
  const ownsTheSong = song.added_by_id === currentUser.value.id

  // User must have priority balance
  const hasPriority = currentUser.value.priority_balance > 0

  // Song must not already be prioritized
  const notAlreadyPrioritized = !song.is_prioritized

  return ownsTheSong && hasPriority && notAlreadyPrioritized
}

function getPrioritizeTooltip(song) {
  if (!currentUser.value) return 'Login required'
  if (song.added_by_id !== currentUser.value.id) {
    return 'You can only prioritize your own songs'
  }
  if (song.is_prioritized) {
    return 'This song is already prioritized'
  }
  if (currentUser.value.priority_balance <= 0) {
    return 'No priority tokens available (earn 1 per day)'
  }
  return 'Use 1 priority token to move this song to the front'
}

async function handlePrioritize(indexInQueue) {
  const fullIndex = props.currentIndex + 1 + indexInQueue

  const accepted = await confirm({
    title: 'Bump this song?',
    message: 'Use 1 priority token to move this song to the front of the queue.',
    confirmLabel: 'Use token'
  })

  if (!accepted) return

  prioritizingIndex.value = indexInQueue

  try {
    await api.prioritizeSong(currentUser.value.id, fullIndex)
  } catch (err) {
    console.error('Prioritize failed:', err)

    let message = 'Failed to prioritize song'
    if (err.message.includes('insufficient')) {
      message = 'You don\'t have enough priority tokens'
    } else if (err.message.includes('ownership') || err.message.includes('own songs')) {
      message = 'You can only prioritize your own songs'
    } else if (err.message.includes('already')) {
      message = 'This song is already prioritized'
    }

    toast.error(message)
  } finally {
    prioritizingIndex.value = null
  }
}

async function handleRemove(indexInQueue) {
  // indexInQueue is index in "Up Next" list, we need index in full "songs" list
  const fullIndex = props.currentIndex + 1 + indexInQueue
  try {
    await api.removeSong(fullIndex, globalStore.currentUser.display_name)
  } catch (err) {
    console.error('Remove song failed:', err)
    if (err.status === 401) {
      toast.error('Session expired. Please log in again.')
      sessionHelper.clearSession()
      globalStore.clearUser()
      router.push({ name: 'Auth' })
    } else if (err.status === 403) {
      toast.error('Permission denied: ' + err.message)
    } else {
      toast.error('Failed to remove song.')
    }
  }
}

async function handleClear() {
  const accepted = await confirm({
    title: 'Clear queue?',
    message: 'Remove all upcoming songs from the queue.',
    confirmLabel: 'Clear queue',
    danger: true
  })

  if (!accepted) return

  try {
    await api.clearQueue(globalStore.currentUser.display_name)
  } catch (err) {
    console.error('Clear queue failed:', err)
    toast.error('Could not clear queue.')
  }
}

function queueIndexFor(upNextIndex) {
  // Convert "Up Next" index to full songs array index
  return props.currentIndex + 1 + upNextIndex
}

function formatDuration(seconds) {
  if (!seconds || seconds < 0) return ''
  const m = Math.floor(seconds / 60)
  const s = Math.floor(seconds % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}
</script>

<style scoped>
.queue-list {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 1.5rem;
}

.queue-header {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  margin-bottom: 1.25rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--navy-border);
}

.queue-header__title-row {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  align-items: center;
  gap: 0.5rem 0.75rem;
  min-width: 0;
}

.queue-header__title-group {
  display: flex;
  align-items: baseline;
  gap: 0.6rem;
  min-width: 0;
  flex: 1 1 auto;
}

.queue-header__title {
  font-size: 1.15rem;
  margin: 0;
  flex-shrink: 0;
}

.queue-header__actions {
  display: flex;
  justify-content: flex-end;
}

.queue-count {
  font-size: 0.78rem;
  color: var(--accent);
  background: rgba(67, 97, 238, 0.15);
  padding: 0.2rem 0.6rem;
  border-radius: var(--radius-full);
  white-space: nowrap;
  flex-shrink: 0;
}

.clear-btn {
  background: rgba(231, 76, 60, 0.1);
  color: #e74c3c;
  border: 1px solid rgba(231, 76, 60, 0.3);
  border-radius: var(--radius-sm);
  padding: 0.2rem 0.55rem;
  font-size: 0.72rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
  white-space: nowrap;
  flex-shrink: 0;
}

.clear-btn:hover {
  background: rgba(231, 76, 60, 0.2);
  border-color: #e74c3c;
}

.queue-items {
  flex-grow: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding-right: 0.5rem;
}

/* Custom Scrollbar */
.queue-items::-webkit-scrollbar {
  width: 6px;
}
.queue-items::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.1);
  border-radius: 4px;
}
.queue-items::-webkit-scrollbar-thumb {
  background: var(--navy-border);
  border-radius: 4px;
}

.queue-item {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding: 0.875rem 1rem;
  background: rgba(7, 7, 13, 0.58);
  border: 1px solid rgba(0, 255, 136, 0.16);
  border-radius: var(--radius-sm);
  clip-path: var(--cyber-chamfer);
  transition: all 0.2s ease;
}

.queue-item:hover {
  background: rgba(0, 255, 136, 0.07);
  border-color: rgba(0, 255, 136, 0.48);
  box-shadow: var(--glow-sm);
  transform: translateX(4px);
}

.item-top-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  width: 100%;
}

.item-number {
  font-weight: 700;
  color: var(--text-muted);
  width: 22px;
  flex-shrink: 0;
  text-align: center;
}

.item-badges {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  flex-shrink: 0;
  min-width: 0;
}
.item-badges:empty {
  display: none;
}

.item-title {
  flex-grow: 1;
  min-width: 0;
  font-weight: 600;
  color: var(--text-main);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.item-meta-row {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding-left: 30px;
  font-size: 0.75rem;
  color: var(--text-muted);
  flex-wrap: wrap;
}
.item-meta-sep {
  opacity: 0.5;
}

.item-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding-top: 0.5rem;
  border-top: 1px dashed rgba(0, 255, 136, 0.12);
}

.priority-indicator {
  display: inline-flex;
  align-items: baseline;
  gap: 0.35rem;
  padding: 0.18rem 0.5rem;
  border: 1px solid rgba(0, 212, 255, 0.22);
  border-radius: var(--radius-sm);
  background: rgba(0, 212, 255, 0.06);
  font-size: 0.7rem;
  line-height: 1;
  white-space: nowrap;
  flex-shrink: 0;
}

.priority-indicator__label {
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.4px;
  font-weight: 600;
}

.priority-indicator__value {
  color: var(--accent-hover);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.prioritize-btn {
  background: rgba(243, 156, 18, 0.1);
  color: #f39c12;
  border: 1px solid rgba(243, 156, 18, 0.3);
  border-radius: var(--radius-sm);
  padding: 0.25rem 0.5rem;
  font-size: 0.7rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
  opacity: 1;
  white-space: nowrap;
}

.auto-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.2rem;
  background: rgba(52, 152, 219, 0.2);
  color: #3498db;
  padding: 0.15rem 0.4rem;
  border-radius: var(--radius-sm);
  font-size: 0.65rem;
  font-weight: 600;
  border: 1px solid rgba(52, 152, 219, 0.3);
}

@media (max-width: 768px) {
  .prioritize-btn {
    font-size: 0.7rem;
    padding: 0.35rem 0.5rem;
  }
}

.prioritize-btn:hover:not(:disabled) {
  background: rgba(243, 156, 18, 0.2);
  border-color: #f39c12;
  transform: scale(1.05);
}

.prioritize-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
  background: rgba(243, 156, 18, 0.05);
}

.prioritize-btn:disabled:hover {
  transform: none;
  background: rgba(243, 156, 18, 0.05);
}

.prioritize-btn.loading {
  opacity: 0.6;
  cursor: wait;
}

.queue-item.prioritized {
  border-left: 3px solid #f39c12;
  background: rgba(243, 156, 18, 0.05);
}

.priority-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 4px;
  background: rgba(243, 156, 18, 0.15);
  color: #f39c12;
  font-size: 0.85rem;
  line-height: 1;
}

.remove-btn {
  background: transparent;
  color: var(--text-muted);
  border: none;
  font-size: 1.5rem;
  line-height: 1;
  cursor: pointer;
  padding: 0;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: all 0.2s ease;
  opacity: 0.65;
  flex-shrink: 0;
}

.queue-item:hover .remove-btn,
.remove-btn:focus-visible {
  opacity: 1;
}

.remove-btn:hover {
  background: rgba(231, 76, 60, 0.1);
  color: #e74c3c;
}

.empty-queue {
  flex-grow: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 0.875rem;
}

/* List Transitions */
.list-move,
.list-enter-active,
.list-leave-active {
  transition: all 0.3s ease;
}

.list-enter-from {
  opacity: 0;
  transform: translateX(-20px);
}

.list-leave-to {
  opacity: 0;
  transform: translateX(20px);
}

.list-leave-active {
  position: absolute;
}

/* Mobile Responsiveness */
@media (max-width: 768px) {
  .queue-item {
    padding: 0.625rem 0.75rem;
    gap: 0.4rem;
  }

  .item-meta-row {
    padding-left: 22px;
  }

  .item-actions {
    justify-content: stretch;
    gap: 0.4rem;
  }

  .item-actions > * {
    flex: 1 1 auto;
    min-width: 0;
  }
}

@media (max-width: 480px) {
  .item-top-row {
    gap: 0.5rem;
  }

  .item-meta-row {
    padding-left: 18px;
  }

  .queue-header__title-row {
    flex-direction: column;
    align-items: flex-start;
    gap: 0.5rem;
  }

  .queue-header__title-group {
    flex: 0 0 auto;
  }

  .queue-header__actions {
    justify-content: flex-start;
  }
}
</style>
