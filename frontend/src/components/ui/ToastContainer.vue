<template>
  <Teleport to="body">
    <div class="toast-region" aria-live="polite" aria-label="Notifications">
      <TransitionGroup name="toast" tag="div" class="toast-list">
        <div
          v-for="toast in toasts"
          :key="toast.id"
          class="toast"
          :class="`toast--${toast.type}`"
          role="status"
        >
          <span>{{ toast.message }}</span>
          <button type="button" class="toast-close" aria-label="Dismiss notification" @click="removeToast(toast.id)">
            &times;
          </button>
        </div>
      </TransitionGroup>
    </div>
  </Teleport>
</template>

<script setup>
import { useToast } from '../../composables/useToast'

const { toasts, removeToast } = useToast()
</script>

<style scoped>
.toast-region {
  position: fixed;
  top: 1rem;
  right: 1rem;
  z-index: 1000;
  pointer-events: none;
}

.toast-list {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  width: min(360px, calc(100vw - 2rem));
}

.toast {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.875rem 1rem;
  color: var(--text-main);
  background: rgba(16, 24, 45, 0.96);
  border: 1px solid var(--navy-border);
  border-radius: var(--radius-md);
  box-shadow: var(--glass-shadow);
  pointer-events: auto;
}

.toast--success {
  border-color: rgba(46, 204, 113, 0.45);
}

.toast--error {
  border-color: rgba(239, 35, 60, 0.5);
}

.toast--info {
  border-color: rgba(76, 201, 240, 0.45);
}

.toast-close {
  background: transparent;
  border: 0;
  color: var(--text-muted);
  font-size: 1.25rem;
  line-height: 1;
  padding: 0;
}

.toast-close:hover,
.toast-close:focus-visible {
  color: var(--text-main);
}

.toast-close:focus-visible {
  outline: 2px solid var(--text-main);
  outline-offset: 2px;
}

.toast-enter-active,
.toast-leave-active {
  transition: all 0.2s ease;
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateX(1rem);
}
</style>
