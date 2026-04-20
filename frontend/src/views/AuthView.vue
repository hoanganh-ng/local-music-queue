<template>
  <div class="auth-container">
    <div class="auth-card glass-panel">
      <div class="auth-header">
        <h1>Local Music Queue</h1>
        <p>Enter your PIN to join the party</p>
      </div>

      <form @submit.prevent="handleLogin" class="auth-form">
        <BaseInput
          id="displayName"
          v-model="displayName"
          label="Display Name (Optional)"
          placeholder="e.g. DJ Sparkles"
        />

        <BaseInput
          id="pin"
          v-model="pin"
          type="password"
          label="PIN Code"
          placeholder="Enter PIN"
          required
        />

        <div v-if="error" class="error-message">
          {{ error }}
        </div>

        <BaseButton
          variant="primary"
          type="submit"
          class="submit-btn"
          :disabled="isLoading"
        >
          {{ isLoading ? 'Connecting...' : 'Join' }}
        </BaseButton>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { globalStore } from '../store'
import { api } from '../services/api'
import BaseInput from '../components/ui/BaseInput.vue'
import BaseButton from '../components/ui/BaseButton.vue'

const router = useRouter()
const pin = ref('')
const displayName = ref('')
const error = ref('')
const isLoading = ref(false)

const handleLogin = async () => {
  if (!pin.value) {
    error.value = "PIN is required"
    return
  }

  error.value = ''
  isLoading.value = true

  try {
    const user = await api.login(pin.value, displayName.value)
    globalStore.setUser(user)
    router.push({ name: 'Dashboard' })
  } catch (err) {
    error.value = err.message || "Failed to authenticate"
  } finally {
    isLoading.value = false
  }
}
</script>

<style scoped>
.auth-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  padding: 1rem;
}

.auth-card {
  width: 100%;
  max-width: 400px;
  padding: 2.5rem 2rem;
  display: flex;
  flex-direction: column;
  gap: 2rem;
  animation: fadeIn 0.5s ease-out;
}

.auth-header {
  text-align: center;
}

.auth-header h1 {
  font-size: 1.75rem;
  color: var(--accent);
  margin-bottom: 0.5rem;
}

.auth-header p {
  color: var(--text-muted);
  font-size: 0.875rem;
}

.auth-form {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}

.submit-btn {
  margin-top: 1rem;
  width: 100%;
}

.error-message {
  color: var(--danger);
  font-size: 0.875rem;
  text-align: center;
  padding: 0.5rem;
  background: rgba(239, 35, 60, 0.1);
  border-radius: var(--radius-sm);
}
</style>
