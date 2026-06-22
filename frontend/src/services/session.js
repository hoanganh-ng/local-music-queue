const TOKEN_KEY = 'lmq_session_token'
const EXPIRY_KEY = 'lmq_session_expires_at'

export const sessionHelper = {
  saveSession(token, expiresAt) {
    if (token) {
      localStorage.setItem(TOKEN_KEY, token)
    }
    if (expiresAt) {
      localStorage.setItem(EXPIRY_KEY, expiresAt)
    }
  },

  getToken() {
    return localStorage.getItem(TOKEN_KEY)
  },

  getExpiresAt() {
    return localStorage.getItem(EXPIRY_KEY)
  },

  clearSession() {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(EXPIRY_KEY)
  },

  isValid() {
    const token = this.getToken()
    const expiresAtStr = this.getExpiresAt()
    if (!token || !expiresAtStr) return false

    const expiresAt = new Date(expiresAtStr)
    return expiresAt > new Date()
  }
}
