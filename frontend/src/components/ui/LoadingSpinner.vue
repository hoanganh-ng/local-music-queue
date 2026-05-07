<template>
  <span class="loading-spinner" :class="sizeClass" aria-hidden="true"></span>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  size: {
    type: String,
    default: 'md'
  }
})

const sizeClass = computed(() => `loading-spinner--${props.size}`)
</script>

<style scoped>
.loading-spinner {
  display: inline-block;
  width: 1.25rem;
  height: 1.25rem;
  border: 2px solid rgba(0, 255, 136, 0.22);
  border-top-color: var(--accent);
  border-right-color: var(--cyber-magenta);
  border-radius: 0;
  clip-path: polygon(25% 0, 75% 0, 100% 25%, 100% 75%, 75% 100%, 25% 100%, 0 75%, 0 25%);
  box-shadow: var(--glow-sm);
  animation: spin 0.8s steps(8) infinite;
}

.loading-spinner--sm {
  width: 1rem;
  height: 1rem;
}

.loading-spinner--lg {
  width: 2rem;
  height: 2rem;
  border-width: 3px;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
