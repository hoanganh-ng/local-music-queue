<template>
  <button
    :disabled="disabled || hasVoted"
    :aria-label="buttonLabel"
    @click="handleVote"
    class="vote-btn"
    :class="{
      'vote-btn--active': !!session,
      'vote-btn--voted': hasVoted,
      'vote-btn--compact': compact
    }"
  >
    <span v-if="!session">
      <template v-if="compact">
        {{ voteType === 'skip' ? 'Skip' : 'Vote' }}
      </template>
      <template v-else>
        {{ voteType === 'skip' ? 'Vote to Skip' : 'Vote to Prioritize' }}
      </template>
    </span>
    <span v-else>
      <template v-if="compact">
        {{ session.voted_by ? Object.keys(session.voted_by).length : 0 }}/{{ session.threshold }}
        <small>({{ countdown }}s)</small>
      </template>
      <template v-else>
        {{ voteType === 'skip' ? 'Skip' : 'Bump' }}?
        {{ session.voted_by ? Object.keys(session.voted_by).length : 0 }}/{{ session.threshold }}
        <small>({{ countdown }}s)</small>
      </template>
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
    },
    compact: {
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

    const buttonLabel = computed(() => {
      const action = props.voteType === 'skip' ? 'skip this song' : 'prioritize this song'
      if (hasVoted.value) return `Voted to ${action}`
      if (session.value) {
        const votes = session.value.voted_by ? Object.keys(session.value.voted_by).length : 0
        return `Vote to ${action}. ${votes} of ${session.value.threshold} votes, ${countdown.value} seconds left`
      }
      return `Vote to ${action}`
    })

    const startCountdown = () => {
      if (countdownInterval) {
        clearInterval(countdownInterval)
      }

      if (session.value) {
        const expiresAt = new Date(session.value.expires_at).getTime()
        const remaining = Math.max(0, Math.ceil((expiresAt - Date.now()) / 1000))
        countdown.value = remaining

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
      buttonLabel,
      countdown,
      handleVote
    }
  }
}
</script>

<style scoped>
.vote-btn {
  padding: 0.4rem 0.75rem;
  border: 1px solid rgba(0, 212, 255, 0.38);
  border-radius: 0;
  background: rgba(0, 212, 255, 0.06);
  backdrop-filter: blur(10px);
  cursor: pointer;
  font-size: 0.8rem;
  font-weight: 800;
  color: var(--accent-hover);
  transition: all 0.2s ease;
  white-space: nowrap;
  clip-path: var(--cyber-chamfer);
}

.vote-btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(255, 255, 255, 0.3);
  color: rgba(255, 255, 255, 0.95);
  transform: translateY(-1px);
}

.vote-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.vote-btn--active {
  background: rgba(255, 0, 255, 0.14);
  border-color: rgba(255, 0, 255, 0.52);
  color: var(--cyber-magenta);
  box-shadow: var(--glow-magenta);
}

.vote-btn--active:hover:not(:disabled) {
  background: rgba(33, 150, 243, 0.2);
  border-color: rgba(33, 150, 243, 0.5);
}

.vote-btn--voted {
  background: rgba(0, 255, 136, 0.14);
  border-color: rgba(0, 255, 136, 0.52);
  color: var(--accent);
  box-shadow: var(--glow);
}

.vote-btn small {
  font-size: 0.75rem;
  opacity: 0.9;
  margin-left: 0.25rem;
}

.vote-btn--compact {
  padding: 0.3rem 0.6rem;
  font-size: 0.75rem;
}

.vote-btn--compact small {
  font-size: 0.65rem;
}
</style>
