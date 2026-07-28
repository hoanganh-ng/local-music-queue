<template>
  <div class="auth-container">
    <div class="auth-card glass-panel">
      <div class="auth-header">
        <h1 class="cyber-glitch">Access Terminal</h1>
        <p>Authenticate to enter Local Music Queue</p>
      </div>

      <div id="g_id_onload"
           :data-client_id="googleClientId"
           data-callback="handleGoogleCallback"
           data-auto_prompt="false">
      </div>

      <div class="g_id_signin"
           data-type="standard"
           data-size="large"
           data-theme="filled_blue"
           data-text="sign_in_with"
           data-shape="rectangular"
           data-logo_alignment="left">
      </div>

      <div v-if="isLoading" class="loading-message" role="status">
        Signing in...
      </div>

      <div v-if="error" class="error-message" role="alert">
        {{ error }}
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { globalStore } from '../store'
import { api } from '../services/api'
import { sessionHelper } from '../services/session'
import { authenticatedLandingRouteName } from '../config/cutover'

const router = useRouter()
const error = ref('')
const isLoading = ref(false)
const googleClientId = import.meta.env.VITE_GOOGLE_CLIENT_ID

onMounted(() => {
  window.handleGoogleCallback = async (response) => {
    error.value = ''
    isLoading.value = true
    try {
      const responseData = await api.loginWithGoogle(response.credential)
      const { session_token, session_expires_at, ...user } = responseData
      sessionHelper.saveSession(session_token, session_expires_at)
      globalStore.setUser(user)
      // R14d: shared cutover-aware landing decision (false mode →
      // Dashboard; true mode → RoomEntry). Same helper as the router
      // guard so the conditional is not duplicated.
      router.push({ name: authenticatedLandingRouteName() })
    } catch (err) {
      error.value = err.message || "Failed to authenticate"
    } finally {
      isLoading.value = false
    }
  }
})
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
  font-size: 1.9rem;
  margin-bottom: 0.5rem;
}

.auth-header p {
  color: var(--text-muted);
  font-size: 0.875rem;
}

.loading-message {
  color: var(--accent-hover);
  font-size: 0.875rem;
  text-align: center;
  padding: 0.5rem;
  background: rgba(76, 201, 240, 0.1);
  border-radius: var(--radius-sm);
}

.error-message {
  color: var(--danger);
  font-size: 0.875rem;
  text-align: center;
  padding: 0.5rem;
  background: rgba(239, 35, 60, 0.1);
  border-radius: var(--radius-sm);
}

#g_id_onload {
  display: none;
}

.g_id_signin {
  display: flex;
  justify-content: center;
}
</style>
