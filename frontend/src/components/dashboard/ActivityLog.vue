<template>
  <div class="activity-log glass-panel">
    <h3 class="header">Activity Log</h3>
    <div class="log-container" ref="logContainer">
      <div v-if="logs.length === 0" class="empty-state">
        No recent activity.
      </div>
      <div
        v-for="(log, idx) in logs"
        :key="idx"
        class="log-entry animate-fade-in"
      >
        <span class="log-time">{{ formatTime(log.timestamp) }}</span>
        <span class="log-message"><strong>{{ log.user }}</strong> {{ log.description }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, nextTick } from 'vue'

const props = defineProps({
  logs: {
    type: Array,
    default: () => []
  }
})

const logContainer = ref(null)

// Auto-scroll to bottom when new logs arrive
watch(() => props.logs, async () => {
  await nextTick()
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight
  }
}, { deep: true })

function formatTime(isoString) {
  if (!isoString) return ''
  const date = new Date(isoString)
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}
</script>

<style scoped>
.activity-log {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 1.5rem;
  overflow: hidden;
}

.header {
  font-size: 1.25rem;
  font-weight: 600;
  margin-bottom: 1rem;
  color: var(--accent);
}

.log-container {
  flex-grow: 1;
  overflow-y: auto;
  padding-right: 0.5rem;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

/* Custom Scrollbar */
.log-container::-webkit-scrollbar {
  width: 6px;
}
.log-container::-webkit-scrollbar-track {
  background: rgba(0, 0, 0, 0.1);
  border-radius: 4px;
}
.log-container::-webkit-scrollbar-thumb {
  background: var(--navy-border);
  border-radius: 4px;
}
.log-container::-webkit-scrollbar-thumb:hover {
  background: var(--accent);
}

.empty-state {
  color: var(--text-muted);
  font-style: italic;
  text-align: center;
  margin-top: 2rem;
}

.log-entry {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  font-size: 0.875rem;
  background: rgba(255, 255, 255, 0.03);
  padding: 0.5rem 0.75rem;
  border-radius: var(--radius-sm);
  border-left: 2px solid var(--accent);
}

.log-time {
  color: var(--text-muted);
  font-size: 0.75rem;
  white-space: nowrap;
}

.log-message {
  color: var(--text-main);
  word-break: break-word;
}

.log-message strong {
  color: var(--accent-hover);
}
</style>
