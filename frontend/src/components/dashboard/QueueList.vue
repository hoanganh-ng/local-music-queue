<template>
  <div class="queue-list glass-panel">
    <div class="queue-header">
      <h3>Up Next</h3>
      <div class="header-controls">
        <span v-if="currentUser" class="priority-badge">
          ⚡ {{ currentUser.priority_balance }}
        </span>
        <button v-if="canControl && queue.length > 0" class="clear-btn" @click="handleClear">Clear</button>
        <span class="queue-count">{{ queue.length }} songs</span>
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
          <div class="item-number">{{ index + 1 }}</div>
          <div class="item-details">
            <div class="item-title">
              <span v-if="song.is_prioritized" class="priority-icon">⚡</span>
              {{ song.title || song.url }}
            </div>
            <div class="item-meta">Added by {{ song.added_by }}</div>
          </div>

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
            {{ prioritizingIndex === index ? 'Prioritizing...' : 'Prioritize' }}
          </button>

          <button v-if="canControl" class="remove-btn" @click="handleRemove(index)" title="Remove from queue">
            &times;
          </button>
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
import { api } from '../../services/api'
import { globalStore } from '../../store'

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

  if (!confirm('Use 1 priority token to move this song to the front?')) {
    return
  }

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

    alert(message)
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
  }
}

async function handleClear() {
  if (confirm('Are you sure you want to clear all upcoming songs?')) {
    try {
      await api.clearQueue(globalStore.currentUser.display_name)
    } catch (err) {
      console.error('Clear queue failed:', err)
    }
  }
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
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1.5rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--navy-border);
}

.queue-header h3 {
  font-size: 1.25rem;
  color: var(--text-main);
  margin: 0;
}

.queue-count {
  font-size: 0.875rem;
  color: var(--accent);
  background: rgba(67, 97, 238, 0.15);
  padding: 0.25rem 0.75rem;
  border-radius: var(--radius-full);
}

.header-controls {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.clear-btn {
  background: rgba(231, 76, 60, 0.1);
  color: #e74c3c;
  border: 1px solid rgba(231, 76, 60, 0.3);
  border-radius: var(--radius-sm);
  padding: 0.2rem 0.6rem;
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
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
  align-items: center;
  gap: 1rem;
  padding: 0.75rem 1rem;
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  transition: all 0.2s ease;
}

.queue-item:hover {
  background: rgba(255, 255, 255, 0.05);
  border-color: var(--navy-border);
  transform: translateX(4px);
}

.item-number {
  font-weight: 700;
  color: var(--text-muted);
  width: 20px;
  text-align: center;
}

.item-details {
  flex-grow: 1;
  min-width: 0;
}

.item-title {
  font-weight: 600;
  color: var(--text-main);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.item-meta {
  font-size: 0.75rem;
  color: var(--text-muted);
  margin-top: 0.25rem;
}

.priority-badge {
  font-size: 0.875rem;
  color: #f39c12;
  background: rgba(243, 156, 18, 0.15);
  padding: 0.25rem 0.75rem;
  border-radius: var(--radius-full);
  font-weight: 600;
}

.prioritize-btn {
  background: rgba(243, 156, 18, 0.1);
  color: #f39c12;
  border: 1px solid rgba(243, 156, 18, 0.3);
  border-radius: var(--radius-sm);
  padding: 0.3rem 0.7rem;
  font-size: 0.75rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
  margin-right: 0.5rem;
  opacity: 1;
}

/* Mobile: smaller sizing */
@media (max-width: 768px) {
  .prioritize-btn {
    font-size: 0.7rem;
    padding: 0.25rem 0.5rem;
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
  color: #f39c12;
  margin-right: 0.25rem;
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
  opacity: 0;
}

.queue-item:hover .remove-btn {
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

.item-details {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.item-title {
  font-size: 1rem;
  font-weight: 500;
  color: var(--text-main);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.item-meta {
  font-size: 0.75rem;
  color: var(--text-muted);
  margin-top: 0.25rem;
}

.remove-btn {
  margin-left: auto;
  background: transparent;
  color: var(--text-muted);
  border: none;
  font-size: 1.25rem;
  padding: 0 0.5rem;
  cursor: pointer;
  opacity: 0;
  transition: all 0.2s ease;
}

.queue-item:hover .remove-btn {
  opacity: 1;
}

.remove-btn:hover {
  color: #e74c3c;
  transform: scale(1.2);
}

.empty-queue {
  flex-grow: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-style: italic;
}

/* Vue Transitions */
.list-enter-active,
.list-leave-active {
  transition: all 0.4s ease;
}
.list-enter-from {
  opacity: 0;
  transform: translateX(30px);
}
.list-leave-to {
  opacity: 0;
  transform: translateX(-30px);
}
</style>
