<template>
  <div class="queue-list glass-panel">
    <div class="queue-header">
      <h3>Up Next</h3>
      <span class="queue-count">{{ queue.length }} songs</span>
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
        </div>
      </TransitionGroup>
    </div>

    <div v-else class="empty-queue">
      Queue is empty.
    </div>
  </div>
</template>

<script setup>
defineProps({
  queue: {
    type: Array,
    default: () => []
  }
})
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
