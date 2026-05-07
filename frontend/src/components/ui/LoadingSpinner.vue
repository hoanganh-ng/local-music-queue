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
  border: 2px solid rgba(76, 201, 240, 0.25);
  border-top-color: var(--accent-hover);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
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
