const TOKEN_KEY = 'lmq_session_token'
const EXPIRY_KEY = 'lmq_session_expires_at'

export const sessionHelper = {
  saveSession(token, expiresAt) {
    if (token) {
      sessionStorage.setItem(TOKEN_KEY, token)
    }
    if (expiresAt) {
      sessionStorage.setItem(EXPIRY_KEY, expiresAt)
    }
  },

  getToken() {
    return sessionStorage.getItem(TOKEN_KEY)
  },

  getExpiresAt() {
    return sessionStorage.getItem(EXPIRY_KEY)
  },

  clearSession() {
    sessionStorage.removeItem(TOKEN_KEY)
    sessionStorage.removeItem(EXPIRY_KEY)
  },

  isValid() {
    const token = this.getToken()
    const expiresAtStr = this.getExpiresAt()
    if (!token || !expiresAtStr) return false

    const expiresAt = new Date(expiresAtStr)
    return expiresAt > new Date()
  }
}
