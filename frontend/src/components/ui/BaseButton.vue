<template>
  <button
    class="base-button"
    :class="{
      'variant-primary': variant === 'primary',
      'variant-secondary': variant === 'secondary',
      'variant-danger': variant === 'danger',
      'is-loading': loading
    }"
    :type="type"
    :disabled="disabled || loading"
    :aria-busy="loading ? 'true' : undefined"
    @click="$emit('click', $event)"
  >
    <slot></slot>
  </button>
</template>

<script setup>
defineProps({
  variant: {
    type: String,
    default: 'primary'
  },
  type: {
    type: String,
    default: 'button'
  },
  disabled: {
    type: Boolean,
    default: false
  },
  loading: {
    type: Boolean,
    default: false
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
  font-weight: 800;
  font-size: 0.9rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  transition: all 0.16s ease;
  backdrop-filter: var(--glass-blur);
  -webkit-backdrop-filter: var(--glass-blur);
  outline: none;
  clip-path: var(--cyber-chamfer);
}

.variant-primary {
  background: rgba(0, 255, 136, 0.08);
  border: 1px solid rgba(0, 255, 136, 0.68);
  color: var(--accent);
  box-shadow: var(--glow-sm);
  text-shadow: 0 0 10px rgba(0, 255, 136, 0.65);
}

.variant-primary:hover:not(:disabled) {
  background: var(--accent);
  color: var(--navy-bg);
  transform: translateY(-2px);
  box-shadow: var(--glow);
  text-shadow: none;
}

.variant-primary:active:not(:disabled) {
  transform: translateY(0);
}

.variant-secondary {
  background: rgba(0, 212, 255, 0.06);
  border: 1px solid rgba(0, 212, 255, 0.45);
  color: var(--accent-hover);
  box-shadow: var(--glow-cyan);
}

.variant-secondary:hover:not(:disabled) {
  background: rgba(0, 212, 255, 0.16);
  transform: translateY(-2px);
}

.variant-secondary:active:not(:disabled) {
  transform: translateY(0);
}

.variant-danger {
  background: rgba(255, 51, 102, 0.08);
  border: 1px solid rgba(255, 51, 102, 0.62);
  color: var(--danger);
  box-shadow: 0 0 10px rgba(255, 51, 102, 0.35);
}

.variant-danger:hover:not(:disabled) {
  background: var(--danger);
  color: var(--navy-bg);
  transform: translateY(-2px);
}

.variant-danger:active:not(:disabled) {
  transform: translateY(0);
}

.base-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
  filter: grayscale(0.8);
  transform: none;
}

.base-button.is-loading {
  cursor: wait;
  transform: none;
}
</style>
