<template>
  <div class="submit-form glass-panel">
    <div class="input-group">
      <div class="input-wrapper">
        <BaseInput
          v-model="inputValue"
          placeholder="Search or paste YouTube URL..."
          @input="handleInput"
          @enter="handleEnter"
          @keydown="handleKeydown"
        />

        <!-- Search Results Dropdown -->
        <div v-if="showResults" class="search-results">
          <div v-if="isSearching" class="search-loading">
            <div class="spinner"></div>
            <span>Searching...</span>
          </div>

          <div v-else-if="searchResults.length === 0" class="no-results">
            No results found
          </div>

          <div
            v-else
            v-for="(result, index) in searchResults"
            :key="result.id"
            class="result-card"
            :class="{ selected: index === selectedIndex }"
            @mouseenter="selectedIndex = index"
          >
            <img
              :src="result.thumbnail || `https://i.ytimg.com/vi/${result.id}/mqdefault.jpg`"
              :alt="result.title"
              class="result-thumbnail"
              @error="e => e.target.src = `https://i.ytimg.com/vi/${result.id}/mqdefault.jpg`"
            />
            <div class="result-info" @click="selectResult(result)">
              <div class="result-title">{{ result.title }}</div>
              <div class="result-meta">
                <span class="result-artist">{{ result.artist }}</span>
                <span class="result-duration">{{ formatDuration(result.duration) }}</span>
              </div>
            </div>
            <button
              class="add-button"
              @click.stop="addResultDirectly(result)"
              :disabled="isLoading"
              title="Add to queue"
            >
              +
            </button>
          </div>
        </div>
      </div>

      <BaseButton
        variant="primary"
        @click="submit()"
        :disabled="isLoading || !inputValue || !isYouTubeUrl"
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
import { ref, computed, onUnmounted } from 'vue'
import BaseInput from '../ui/BaseInput.vue'
import BaseButton from '../ui/BaseButton.vue'
import { api } from '../../services/api'

const emit = defineEmits(['submit'])

const inputValue = ref('')
const isLoading = ref(false)
const error = ref('')
const pendingUrl = ref(null)
const completionTimeout = ref(null)

// Search state
const searchResults = ref([])
const isSearching = ref(false)
const showResults = ref(false)
const selectedIndex = ref(-1)
const searchDebounceTimer = ref(null)

const ytRegex = /^(https?\:\/\/)?(www\.youtube\.com|youtu\.?be)\/.+$/

const isYouTubeUrl = computed(() => {
  return ytRegex.test(inputValue.value.trim())
})

const handleInput = () => {
  clearTimeout(searchDebounceTimer.value)

  if (isYouTubeUrl.value) {
    showResults.value = false
    searchResults.value = []
    return
  }

  const query = inputValue.value.trim()
  if (query.length < 2) {
    showResults.value = false
    searchResults.value = []
    return
  }

  searchDebounceTimer.value = setTimeout(() => {
    performSearch(query)
  }, 500)
}

const performSearch = async (query) => {
  if (!query) return

  isSearching.value = true
  showResults.value = true
  selectedIndex.value = -1

  try {
    const results = await api.searchYouTube(query)
    searchResults.value = results || []
  } catch (e) {
    console.error('Search failed:', e)
    searchResults.value = []
  } finally {
    isSearching.value = false
  }
}

const selectResult = (result) => {
  inputValue.value = result.url
  showResults.value = false
  searchResults.value = []
  selectedIndex.value = -1
}

const addResultDirectly = async (result) => {
  if (isLoading.value) return

  inputValue.value = result.url
  showResults.value = false
  searchResults.value = []
  selectedIndex.value = -1

  await submit(result)
}

const handleKeydown = (event) => {
  if (!showResults.value || searchResults.value.length === 0) return

  if (event.key === 'ArrowDown') {
    event.preventDefault()
    selectedIndex.value = Math.min(selectedIndex.value + 1, searchResults.value.length - 1)
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    selectedIndex.value = Math.max(selectedIndex.value - 1, -1)
  } else if (event.key === 'Escape') {
    showResults.value = false
    selectedIndex.value = -1
  }
}

const handleEnter = () => {
  if (showResults.value && selectedIndex.value >= 0 && searchResults.value[selectedIndex.value]) {
    selectResult(searchResults.value[selectedIndex.value])
  } else {
    submit()
  }
}

const formatDuration = (seconds) => {
  const mins = Math.floor(seconds / 60)
  const secs = seconds % 60
  return `${mins}:${secs.toString().padStart(2, '0')}`
}

const handleSongAdded = (song) => {
  if (!pendingUrl.value) return

  if (normalizeUrl(song.url) === normalizeUrl(pendingUrl.value)) {
    clearTimeout(completionTimeout.value)
    inputValue.value = ''
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

const submit = async (metadata = null) => {
  if (!inputValue.value.trim() || !isYouTubeUrl.value) return

  error.value = ''
  isLoading.value = true
  pendingUrl.value = inputValue.value.trim()
  showResults.value = false

  try {
    emit('submit', pendingUrl.value, metadata)

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
  if (searchDebounceTimer.value) {
    clearTimeout(searchDebounceTimer.value)
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

.input-wrapper {
  flex-grow: 1;
  position: relative;
}

.search-results {
  position: absolute;
  bottom: calc(100% + 0.5rem);
  left: 0;
  right: 0;
  background: rgba(20, 25, 45, 0.98);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(100, 150, 255, 0.3);
  border-radius: 0.5rem;
  max-height: 350px;
  overflow-y: auto;
  z-index: 100;
  box-shadow: 0 -8px 32px rgba(0, 0, 0, 0.4);
}

.search-results::-webkit-scrollbar {
  width: 6px;
}

.search-results::-webkit-scrollbar-track {
  background: transparent;
}

.search-results::-webkit-scrollbar-thumb {
  background: rgba(100, 150, 255, 0.2);
  border-radius: 3px;
}

.search-results::-webkit-scrollbar-thumb:hover {
  background: rgba(100, 150, 255, 0.4);
}

.search-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  padding: 2rem;
  color: var(--text-secondary);
}

.spinner {
  width: 20px;
  height: 20px;
  border: 2px solid rgba(100, 150, 255, 0.3);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.no-results {
  padding: 2rem;
  text-align: center;
  color: var(--text-secondary);
}

.result-card {
  display: flex;
  gap: 1rem;
  padding: 0.75rem;
  cursor: pointer;
  transition: background 0.2s ease;
  border-bottom: 1px solid rgba(100, 150, 255, 0.1);
  align-items: center;
}

.result-card:last-child {
  border-bottom: none;
}

.result-card:hover,
.result-card.selected {
  background: rgba(100, 150, 255, 0.1);
}

.result-thumbnail {
  width: 120px;
  height: 90px;
  object-fit: cover;
  border-radius: 0.25rem;
  flex-shrink: 0;
}

.result-info {
  flex-grow: 1;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  min-width: 0;
  cursor: pointer;
}

.result-title {
  font-weight: 500;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.result-meta {
  display: flex;
  gap: 1rem;
  font-size: 0.875rem;
  color: var(--text-secondary);
}

.result-artist {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.result-duration {
  flex-shrink: 0;
}

.add-button {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 2px solid rgba(100, 150, 255, 0.5);
  background: rgba(100, 150, 255, 0.1);
  color: var(--accent);
  font-size: 1.25rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
  display: flex;
  align-items: center;
  justify-content: center;
}

.add-button:hover:not(:disabled) {
  background: rgba(100, 150, 255, 0.2);
  border-color: var(--accent);
  transform: scale(1.1);
}

.add-button:active:not(:disabled) {
  transform: scale(0.95);
}

.add-button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.error-message {
  color: var(--danger);
  font-size: 0.875rem;
  margin-top: 0.25rem;
  animation: fadeIn 0.3s ease;
}

@media (max-width: 768px) {
  .result-thumbnail {
    width: 80px;
    height: 60px;
  }
}
</style>
