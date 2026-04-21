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
import { ref, onUnmounted } from 'vue'
import BaseInput from '../ui/BaseInput.vue'
import BaseButton from '../ui/BaseButton.vue'

const emit = defineEmits(['submit'])

const url = ref('')
const isLoading = ref(false)
const error = ref('')
const pendingUrl = ref(null)
const completionTimeout = ref(null)

const handleSongAdded = (song) => {
  if (!pendingUrl.value) return

  if (normalizeUrl(song.url) === normalizeUrl(pendingUrl.value)) {
    clearTimeout(completionTimeout.value)
    url.value = ''
    isLoading.value = false
    pendingUrl.value = null
    error.value = ''
  }
}

const normalizeUrl = (urlString) => {
  try {
    const url = new URL(urlString)
    if (url.hostname.includes('youtube.com')) {
      return url.searchParams.get('v') || urlString
    } else if (url.hostname.includes('youtu.be')) {
      return url.pathname.slice(1) || urlString
    }
    return urlString
  } catch {
    return urlString
  }
}

const submit = async () => {
  if (!url.value.trim()) return

  const ytRegex = /^(https?\:\/\/)?(www\.youtube\.com|youtu\.?be)\/.+$/
  if (!ytRegex.test(url.value.trim())) {
    error.value = "Please enter a valid YouTube URL."
    return
  }

  error.value = ''
  isLoading.value = true
  pendingUrl.value = url.value.trim()

  try {
    await emit('submit', pendingUrl.value)

    completionTimeout.value = setTimeout(() => {
      if (pendingUrl.value) {
        error.value = "Request timed out. Please try again."
        isLoading.value = false
        pendingUrl.value = null
      }
    }, 45000)

  } catch (e) {
    error.value = e.message || "Failed to add song. Please try again."
    isLoading.value = false
    pendingUrl.value = null
  }
}

onUnmounted(() => {
  if (completionTimeout.value) {
    clearTimeout(completionTimeout.value)
  }
})

defineExpose({
  handleSongAdded
})
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
