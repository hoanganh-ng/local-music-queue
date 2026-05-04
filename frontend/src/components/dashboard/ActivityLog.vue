<template>
  <div class="activity-log glass-panel">
    <h3 class="header">Activity Log</h3>
    <div class="log-container" ref="logContainer">
      <div v-if="logs.length === 0" class="empty-state">
        No recent activity.
      </div>
      <TransitionGroup v-else name="log" tag="div">
        <div v-for="log in logs" :key="log.key" class="log-entry">
          <div class="message-bubble">
            <span class="user-name">{{ log.user }}</span>
            <span class="description">{{ log.description }}</span>
          </div>
        </div>
      </TransitionGroup>
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

// Auto-scroll to top when new logs arrive
watch(() => props.logs, async () => {
  await nextTick()
  if (logContainer.value) {
    logContainer.value.scrollTo({
      top: 0,
      behavior: 'smooth'
    })
  }
}, { deep: true })
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
  flex-direction: column;
  align-items: flex-start;
  margin-bottom: 0.5rem;
}

.message-bubble {
  background: rgba(255, 255, 255, 0.08);
  padding: 0.75rem 1rem;
  border-radius: 12px;
  border-bottom-left-radius: 4px;
  max-width: 90%;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
  transition: transform 0.2s ease;
}

.message-bubble:hover {
  transform: translateY(-2px);
  background: rgba(255, 255, 255, 0.12);
}

.user-name {
  display: block;
  font-size: 0.75rem;
  font-weight: 700;
  color: var(--accent);
  margin-bottom: 0.25rem;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.description {
  color: var(--text-main);
  font-size: 0.9rem;
  line-height: 1.4;
  word-break: break-word;
}

/* TransitionGroup animations */
.log-enter-active {
  transition: all 0.4s ease-out;
}

.log-enter-from {
  opacity: 0;
  transform: translateY(-10px);
}

.log-enter-to {
  opacity: 1;
  transform: translateY(0);
}

.log-move {
  transition: transform 0.4s ease;
}
</style>
