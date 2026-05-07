<template>
  <div class="input-wrapper">
    <label v-if="label" :for="id" class="input-label">{{ label }}</label>
    <span class="terminal-prefix" aria-hidden="true">&gt;</span>
    <input
      :id="id"
      class="base-input glass-panel"
      :type="type"
      :placeholder="placeholder"
      :value="modelValue"
      :role="role"
      :aria-expanded="ariaExpanded"
      :aria-controls="ariaControls"
      :aria-activedescendant="ariaActivedescendant"
      :aria-autocomplete="ariaAutocomplete"
      @input="handleInput"
      @keydown="$emit('keydown', $event)"
      @keyup.enter="$emit('enter', $event)"
    />
  </div>
</template>

<script setup>
const emit = defineEmits(['update:modelValue', 'input', 'enter', 'keydown'])

function handleInput(event) {
  emit('update:modelValue', event.target.value)
  emit('input', event)
}

defineProps({
  modelValue: {
    type: [String, Number],
    default: ''
  },
  label: {
    type: String,
    default: ''
  },
  id: {
    type: String,
    default: () => `input-${Math.random().toString(36).substring(2, 9)}`
  },
  type: {
    type: String,
    default: 'text'
  },
  placeholder: {
    type: String,
    default: ''
  },
  role: {
    type: String,
    default: undefined
  },
  ariaExpanded: {
    type: [String, Boolean],
    default: undefined
  },
  ariaControls: {
    type: String,
    default: undefined
  },
  ariaActivedescendant: {
    type: String,
    default: undefined
  },
  ariaAutocomplete: {
    type: String,
    default: undefined
  }
})

</script>

<style scoped>
.input-wrapper {
  position: relative;
  display: flex;
  flex-direction: column;
  width: 100%;
}

.input-label {
  font-size: 0.78rem;
  color: var(--accent-hover);
  margin-bottom: 0.5rem;
  font-weight: 800;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.terminal-prefix {
  position: absolute;
  left: 0.95rem;
  bottom: 0.72rem;
  z-index: 1;
  color: var(--accent);
  font-weight: 900;
  text-shadow: var(--glow-sm);
}

.base-input {
  width: 100%;
  padding: 0.75rem 1rem 0.75rem 2rem;
  font-size: 1rem;
  color: var(--text-main);
  background: var(--surface-inset);
  border: 1px solid rgba(0, 255, 136, 0.35);
  border-radius: var(--radius-sm);
  transition: all 0.18s ease;
  outline: none;
  clip-path: var(--cyber-chamfer);
}

.base-input::placeholder {
  color: rgba(138, 166, 154, 0.65);
}

.base-input:focus {
  border-color: var(--accent);
  box-shadow: var(--focus-ring);
  background: rgba(7, 7, 13, 0.9);
}
</style>
