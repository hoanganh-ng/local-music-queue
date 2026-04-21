<template>
  <div class="queue-list glass-panel">
    <div class="queue-header">
      <h3>Up Next</h3>
      <div class="header-controls">
        <button v-if="isHost && queue.length > 0" class="clear-btn" @click="handleClear">Clear</button>
        <span class="queue-count">{{ queue.length }} songs</span>
      </div>
    </div>

    <div class="queue-items" v-if="queue.length > 0">
      <TransitionGroup name="list">
        <div 
          v-for="(song, index) in queue" 
          :key="song.id || index"
          class="queue-item"
        >
          <div class="item-number">{{ index + 1 }}</div>
          <div class="item-details">
            <div class="item-title">{{ song.title || song.url }}</div>
            <div class="item-meta">Added by {{ song.added_by }}</div>
          </div>
          <button v-if="isHost" class="remove-btn" @click="handleRemove(index)" title="Remove from queue">
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
  }
})

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
