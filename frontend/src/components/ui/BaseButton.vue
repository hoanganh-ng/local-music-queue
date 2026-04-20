<template>
  <button
    class="base-button"
    :class="{
      'variant-primary': variant === 'primary',
      'variant-secondary': variant === 'secondary',
      'variant-danger': variant === 'danger'
    }"
    @click="$emit('click', $event)"
  >
    <slot></slot>
  </button>
</template>

<script setup>
defineProps({
  variant: {
    type: String,
    default: 'primary' // 'primary', 'secondary', 'danger'
  }
})

defineEmits(['click'])
</script>

<style scoped>
.base-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0.75rem 1.5rem;
  border-radius: var(--radius-sm);
  font-weight: 600;
  font-size: 1rem;
  transition: all 0.2s ease;
  backdrop-filter: var(--glass-blur);
  -webkit-backdrop-filter: var(--glass-blur);
  outline: none;
}

.variant-primary {
  background: rgba(67, 97, 238, 0.2);
  border: 1px solid rgba(67, 97, 238, 0.4);
  color: var(--accent-hover);
  box-shadow: 0 4px 15px rgba(67, 97, 238, 0.15);
}

.variant-primary:hover {
  background: rgba(67, 97, 238, 0.4);
  border-color: rgba(67, 97, 238, 0.6);
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(67, 97, 238, 0.25);
}

.variant-primary:active {
  transform: translateY(0);
}

.variant-secondary {
  background: var(--glass-bg);
  border: var(--glass-border);
  color: var(--text-main);
}

.variant-secondary:hover {
  background: var(--navy-panel-hover);
  transform: translateY(-2px);
}

.variant-secondary:active {
  transform: translateY(0);
}

.variant-danger {
  background: rgba(239, 35, 60, 0.2);
  border: 1px solid rgba(239, 35, 60, 0.4);
  color: #ff4d6d;
}

.variant-danger:hover {
  background: rgba(239, 35, 60, 0.4);
  transform: translateY(-2px);
}

.variant-danger:active {
  transform: translateY(0);
}

.base-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
  transform: none;
}
</style>
