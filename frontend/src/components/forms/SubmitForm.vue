<template>
  <div class="submit-form glass-panel">
    <div class="input-group">
      <BaseInput
        v-model="url"
        placeholder="Paste YouTube URL here..."
        @enter="submit"
      />
      <BaseButton 
        variant="primary" 
        @click="submit"
        :disabled="isLoading || !url"
      >
        <span v-if="isLoading">Adding...</span>
        <span v-else>Add to Queue</span>
      </BaseButton>
    </div>
    <div v-if="error" class="error-message">
      {{ error }}
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import BaseInput from '../ui/BaseInput.vue'
import BaseButton from '../ui/BaseButton.vue'

const emit = defineEmits(['submit'])

const url = ref('')
const isLoading = ref(false)
const error = ref('')

const submit = async () => {
  if (!url.value.trim()) return

  // Basic youtube url validation
  const ytRegex = /^(https?\:\/\/)?(www\.youtube\.com|youtu\.?be)\/.+$/
  if (!ytRegex.test(url.value.trim())) {
    error.value = "Please enter a valid YouTube URL."
    return
  }

  error.value = ''
  isLoading.value = true

  try {
    emit('submit', url.value.trim())
    url.value = '' // Clear on successful submit init
  } finally {
    // Parent should handle actual loading state if needed, but we reset here quickly
    setTimeout(() => {
      isLoading.value = false
    }, 500)
  }
}
</script>

<style scoped>
.submit-form {
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.input-group {
  display: flex;
  gap: 1rem;
  align-items: stretch;
}

/* BaseInput takes up remaining space */
.input-group > :first-child {
  flex-grow: 1;
}

.error-message {
  color: var(--danger);
  font-size: 0.875rem;
  margin-top: 0.25rem;
  animation: fadeIn 0.3s ease;
}
</style>
