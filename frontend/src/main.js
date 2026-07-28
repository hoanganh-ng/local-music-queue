import { createApp } from 'vue'
import './style.css'
import App from './App.vue'
import router from './router'
import { roomCutoverAuthoritative } from './config/cutover'
import { globalStore } from './store'
import { wsClient } from './services/websocket'

// R14d: true (post-cutover) artifact bootstrap. Retire the legacy
// global runtime slices via the single store operation, then
// intentionally disconnect the global WebSocket client (connect() is
// already a no-op in true mode, so it stays disabled). The false
// artifact changes nothing: existing global state is not reset and
// the global WebSocket is not force-disconnected.
if (roomCutoverAuthoritative) {
  globalStore.retireLegacyGlobalRuntimeState()
  wsClient.disconnect()
}

const app = createApp(App)
app.use(router)
app.mount('#app')
