<template>
  <div class="activity-log glass-panel">
    <h3 class="header cyber-glitch">Activity Log</h3>
    <div class="log-container" ref="logContainer">
      <div v-if="logs.length === 0" class="empty-state">
        No recent activity.
      </div>
      <TransitionGroup v-else name="log" tag="div">
        <div
          v-for="log in logs"
          :key="log.key"
          class="log-entry"
          :class="{ 'log-entry--mine': isCurrentUserLog(log) }"
        >
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
  },
  currentUserName: {
    type: String,
    default: ''
  }
})

const logContainer = ref(null)

const isCurrentUserLog = (log) => {
  return props.currentUserName && log.user === props.currentUserName
}

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
  min-height: 0;
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
  flex: 1 1 auto;
  min-height: 0;
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

.log-entry--mine {
  align-items: flex-end;
}

.log-entry--mine .message-bubble {
  border-color: rgba(0, 255, 136, 0.34);
  box-shadow: var(--glow-sm);
  text-align: right;
}

.log-entry--mine .user-name {
  color: var(--accent);
}

.message-bubble {
  background: rgba(0, 212, 255, 0.06);
  padding: 0.75rem 1rem;
  border: 1px solid rgba(0, 212, 255, 0.22);
  border-radius: 0;
  max-width: 90%;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
  transition: transform 0.2s ease, border-color 0.2s ease;
  clip-path: var(--cyber-chamfer);
}

.message-bubble:hover {
  transform: translateY(-2px);
  border-color: rgba(0, 212, 255, 0.55);
  box-shadow: var(--glow-cyan);
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
