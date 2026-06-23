<template>
  <div ref="formRef" class="submit-form glass-panel">
    <div class="input-group">
      <div class="input-wrapper">
        <BaseInput
          v-model="inputValue"
          placeholder="Search or paste YouTube URL..."
          @input="handleInput"
          @enter="handleEnter"
          role="combobox"
          aria-autocomplete="list"
          :aria-expanded="showResults"
          aria-controls="search-results-list"
          :aria-activedescendant="activeDescendant"
          @keydown="handleKeydown"
        />

        <div
          v-if="showResults"
          id="search-results-list"
          class="search-results"
          :class="{ 'search-results--above': resultsAbove }"
          role="listbox"
          aria-label="YouTube search results"
        >
          <div v-if="isSearching" class="search-loading" role="status">
            <LoadingSpinner size="sm" />
            <span>Searching...</span>
          </div>

          <div v-else-if="searchResults.length === 0" class="no-results">
            No results found
          </div>

          <div
            v-else
            v-for="(result, index) in searchResults"
            :id="resultOptionId(index)"
            :key="result.id"
            class="result-card"
            :class="{ selected: index === selectedIndex }"
            role="option"
            :aria-selected="index === selectedIndex"
            tabindex="-1"
            @click="selectResult(result)"
            @mouseenter="selectedIndex = index"
          >
            <img
              :src="result.thumbnail || `https://i.ytimg.com/vi/${result.id}/mqdefault.jpg`"
              :alt="result.title"
              class="result-thumbnail"
              @error="handleThumbnailError($event, result.id)"
            />
            <div class="result-info">
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
              :aria-label="`Add ${result.title} to queue`"
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
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'
import BaseInput from '../ui/BaseInput.vue'
import BaseButton from '../ui/BaseButton.vue'
import LoadingSpinner from '../ui/LoadingSpinner.vue'
import { api } from '../../services/api'
import { useToast } from '../../composables/useToast'

const emit = defineEmits(['submit'])
const toast = useToast()

const props = defineProps({
  onSubmit: {
    type: Function,
    required: false
  }
})

const formRef = ref(null)
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
const resultsAbove = ref(false)

const ytRegex = /^(https?\:\/\/)?(www\.youtube\.com|youtu\.?be)\/.+$/

const isYouTubeUrl = computed(() => {
  return ytRegex.test(inputValue.value.trim())
})

const activeDescendant = computed(() => {
  return selectedIndex.value >= 0 ? resultOptionId(selectedIndex.value) : undefined
})

const resultOptionId = (index) => `search-result-${index}`

const closeResults = () => {
  showResults.value = false
  selectedIndex.value = -1
}

const updateResultsPlacement = async () => {
  await nextTick()
  const input = formRef.value?.querySelector('input')
  if (!input) return

  const rect = input.getBoundingClientRect()
  const spaceBelow = window.innerHeight - rect.bottom
  const spaceAbove = rect.top
  resultsAbove.value = spaceBelow < 360 && spaceAbove > spaceBelow
}

const handleClickOutside = (event) => {
  if (formRef.value && !formRef.value.contains(event.target)) {
    closeResults()
  }
}

const handleViewportChange = () => {
  if (showResults.value) updateResultsPlacement()
}

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
  updateResultsPlacement()

  try {
    const results = await api.searchYouTube(query)
    searchResults.value = results || []
  } catch (e) {
    console.error('Search failed:', e)
    searchResults.value = []
    toast.error('Search failed. Try again.')
  } finally {
    isSearching.value = false
  }
}

const selectResult = (result) => {
  inputValue.value = result.url
  closeResults()
}

const addResultDirectly = async (result) => {
  if (isLoading.value) return

  inputValue.value = result.url
  closeResults()

  await submit(result)
}

const handleKeydown = (event) => {
  if (!showResults.value || searchResults.value.length === 0) return

  if (event.key === 'ArrowDown') {
    event.preventDefault()
    selectedIndex.value = (selectedIndex.value + 1) % searchResults.value.length
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    selectedIndex.value = selectedIndex.value <= 0 ? searchResults.value.length - 1 : selectedIndex.value - 1
  } else if (event.key === 'Escape') {
    closeResults()
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

const handleThumbnailError = (event, id) => {
  event.target.src = `https://i.ytimg.com/vi/${id}/mqdefault.jpg`
  event.target.alt = 'Thumbnail unavailable'
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
    // Call the onSubmit prop if provided (for direct async handling)
    if (props.onSubmit) {
      await props.onSubmit(pendingUrl.value, metadata)
    } else {
      // Fallback to emit for backward compatibility
      emit('submit', pendingUrl.value, metadata)
    }

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

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  window.addEventListener('resize', handleViewportChange)
  window.addEventListener('scroll', handleViewportChange, true)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  window.removeEventListener('resize', handleViewportChange)
  window.removeEventListener('scroll', handleViewportChange, true)
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
  padding: 1rem 1.25rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  overflow: visible;
  clip-path: none;
  z-index: 20;
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
  top: calc(100% + 0.5rem);
  left: 0;
  right: 0;
  background: rgba(7, 7, 13, 0.98);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(0, 255, 136, 0.38);
  border-radius: 0;
  clip-path: var(--cyber-chamfer);
  max-height: 350px;
  overflow-y: auto;
  z-index: 100;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.4);
}

.search-results--above {
  top: auto;
  bottom: calc(100% + 0.5rem);
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
  color: var(--text-muted);
}


.no-results {
  padding: 2rem;
  text-align: center;
  color: var(--text-muted);
}

.result-card {
  display: flex;
  gap: 1rem;
  padding: 0.75rem;
  cursor: pointer;
  transition: background 0.2s ease;
  border-bottom: 1px solid rgba(0, 255, 136, 0.12);
  align-items: center;
}

.result-card:last-child {
  border-bottom: none;
}

.result-card:hover,
.result-card.selected {
  background: rgba(0, 255, 136, 0.1);
  box-shadow: inset 3px 0 0 var(--accent);
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
  color: var(--text-main);
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
  color: var(--text-muted);
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
  .input-group {
    flex-direction: column;
  }

  .result-card {
    gap: 0.75rem;
  }

  .result-thumbnail {
    width: 80px;
    height: 60px;
  }
}
</style>
