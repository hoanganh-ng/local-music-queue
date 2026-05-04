<template>
  <button
    :disabled="disabled || hasVoted"
    @click="handleVote"
    class="vote-btn"
    :class="{ 'vote-btn--active': !!session, 'vote-btn--voted': hasVoted }"
  >
    <span v-if="!session">
      {{ voteType === 'skip' ? 'Vote to Skip' : 'Vote to Prioritize' }}
    </span>
    <span v-else>
      {{ voteType === 'skip' ? 'Skip' : 'Bump' }}?
      {{ session.voted_by ? Object.keys(session.voted_by).length : 0 }}/{{ session.threshold }}
      <small>({{ countdown }}s)</small>
    </span>
  </button>
</template>

<script>
import { computed, ref, watch, onMounted, onUnmounted } from 'vue'
import { globalStore } from '../../store'
import { api } from '../../services/api'

export default {
  name: 'VoteButton',
  props: {
    voteType: {
      type: String,
      required: true,
      validator: (value) => ['skip', 'prioritize'].includes(value)
    },
    songID: {
      type: String,
      required: true
    },
    songIndex: {
      type: Number,
      default: -1
    },
    disabled: {
      type: Boolean,
      default: false
    }
  },
  setup(props) {
    const countdown = ref(0)
    let countdownInterval = null

    const session = computed(() => {
      if (props.voteType === 'skip') {
        return globalStore.skipSessionFor(props.songID)
      } else {
        return globalStore.prioritySessionFor(props.songID)
      }
    })

    const hasVoted = computed(() => {
      if (!session.value || !globalStore.currentUser) return false
      return session.value.voted_by && session.value.voted_by[globalStore.currentUser.id] === true
    })

    const startCountdown = () => {
      if (countdownInterval) {
        clearInterval(countdownInterval)
      }

      if (session.value) {
        countdown.value = session.value.remaining_seconds || 0

        countdownInterval = setInterval(() => {
          if (countdown.value > 0) {
            countdown.value--
          } else {
            clearInterval(countdownInterval)
            countdownInterval = null
          }
        }, 1000)
      }
    }

    const stopCountdown = () => {
      if (countdownInterval) {
        clearInterval(countdownInterval)
        countdownInterval = null
      }
      countdown.value = 0
    }

    watch(session, (newSession) => {
      if (newSession) {
        startCountdown()
      } else {
        stopCountdown()
      }
    }, { immediate: true })

    onMounted(() => {
      if (session.value) {
        startCountdown()
      }
    })

    onUnmounted(() => {
      stopCountdown()
    })

    const handleVote = async () => {
      const user = globalStore.currentUser
      if (!user) return

      try {
        if (props.voteType === 'skip') {
          await api.castSkipVote(user.id, user.role)
        } else {
          await api.castPriorityVote(user.id, user.role, props.songIndex)
        }
      } catch (err) {
        if (err.message.includes('409')) {
          console.log('Already voted')
        } else {
          console.error('Vote failed:', err)
        }
      }
    }

    return {
      session,
      hasVoted,
      countdown,
      handleVote
    }
  }
}
</script>

<style scoped>
.vote-btn {
  padding: 0.5rem 1rem;
  border: 1px solid #ccc;
  border-radius: 4px;
  background: white;
  cursor: pointer;
  font-size: 0.875rem;
  transition: all 0.2s;
}

.vote-btn:hover:not(:disabled) {
  background: #f0f0f0;
  border-color: #999;
}

.vote-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.vote-btn--active {
  background: #e3f2fd;
  border-color: #2196f3;
  color: #1976d2;
}

.vote-btn--voted {
  background: #c8e6c9;
  border-color: #4caf50;
  color: #2e7d32;
}

.vote-btn small {
  font-size: 0.75rem;
  opacity: 0.8;
}
</style>
