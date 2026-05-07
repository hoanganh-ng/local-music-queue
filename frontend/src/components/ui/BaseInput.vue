<template>
  <div class="input-wrapper">
    <label v-if="label" :for="id" class="input-label">{{ label }}</label>
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
  display: flex;
  flex-direction: column;
  width: 100%;
}

.input-label {
  font-size: 0.875rem;
  color: var(--text-muted);
  margin-bottom: 0.5rem;
  font-weight: 500;
}

.base-input {
  width: 100%;
  padding: 0.75rem 1rem;
  font-size: 1rem;
  color: var(--text-main);
  background: rgba(10, 14, 23, 0.4); /* Darker inset look */
  border: 1px solid var(--navy-border);
  border-radius: var(--radius-sm);
  transition: all 0.2s ease;
  outline: none;
}

.base-input::placeholder {
  color: rgba(138, 155, 179, 0.5);
}

.base-input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px rgba(67, 97, 238, 0.2);
  background: rgba(10, 14, 23, 0.6);
}
</style>
