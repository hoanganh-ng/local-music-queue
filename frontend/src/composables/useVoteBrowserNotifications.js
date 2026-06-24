import { computed, ref } from 'vue'

const DISMISSED_KEY = 'lmq_browser_notifications_dismissed'

function readDismissed() {
  try {
    return localStorage.getItem(DISMISSED_KEY) === 'true'
  } catch {
    return false
  }
}

function writeDismissed(value) {
  try {
    localStorage.setItem(DISMISSED_KEY, value ? 'true' : 'false')
  } catch {
    /* storage unavailable; in-memory only */
  }
}

function detectNotificationCtor() {
  if (typeof window === 'undefined') return null
  if (typeof window.Notification !== 'function') return null
  return window.Notification
}

const unsupported = ref(detectNotificationCtor() === null)

// permission mirrors Notification.permission.
// 'default' means the browser hasn't been asked yet — show a one-time CTA.
// 'granted' means enabled automatically.
// 'denied' means never ask again; CTA stays hidden.
const permission = ref(
  detectNotificationCtor() ? window.Notification.permission : 'default'
)

const dismissed = ref(readDismissed())

function syncPermissionFromBrowser() {
  const Ctor = detectNotificationCtor()
  if (!Ctor) {
    permission.value = 'default'
    return
  }
  permission.value = window.Notification.permission
}

let installed = false
if (typeof document !== 'undefined' && !installed) {
  installed = true
  document.addEventListener('visibilitychange', syncPermissionFromBrowser)
}

// CTA visibility: only when we can plausibly ask and haven't been dismissed.
const showCta = computed(() => {
  if (unsupported.value) return false
  if (dismissed.value) return false
  return permission.value === 'default'
})

// canNotify: granted (auto-enabled). There is no user opt-out toggle by
// design — granting means opting in. document.hidden is read directly inside
// notifyVote at call-time because it's not a reactive value and we don't want
// a stale computed when the tab transitions between hidden and visible.
const canNotify = computed(() => {
  if (unsupported.value) return false
  if (permission.value !== 'granted') return false
  return true
})

async function requestPermission() {
  const Ctor = detectNotificationCtor()
  if (!Ctor) {
    permission.value = 'denied'
    writeDismissed(true)
    dismissed.value = true
    return 'denied'
  }
  if (Ctor.permission !== 'default') {
    permission.value = Ctor.permission
    if (Ctor.permission !== 'granted') {
      writeDismissed(true)
      dismissed.value = true
    }
    return Ctor.permission
  }
  let result
  try {
    result = await Ctor.requestPermission()
  } catch {
    // Defensive: browser may reject or throw if the prompt was dismissed,
    // closed, or the API isn't actually usable despite detection.
    permission.value = 'denied'
    writeDismissed(true)
    dismissed.value = true
    return 'denied'
  }
  permission.value = result
  // Whether granted or denied, the user has answered — don't re-prompt.
  writeDismissed(true)
  dismissed.value = true
  return result
}

function dismissCta() {
  dismissed.value = true
  writeDismissed(true)
}

function notifyVote(tag, title, body) {
  if (!canNotify.value) return
  // document.hidden is non-reactive; read it at call-time so visibility
  // transitions between hidden and visible are honored immediately.
  if (typeof document !== 'undefined' && document.hidden !== true) return
  const Ctor = detectNotificationCtor()
  if (!Ctor) return
  try {
    const notification = new Ctor(title, { body, tag })
    notification.onclick = () => {
      try {
        if (typeof window !== 'undefined' && typeof window.focus === 'function') {
          window.focus()
        }
      } catch {
        /* noop */
      }
      try {
        notification.close()
      } catch {
        /* noop */
      }
    }
  } catch {
    /* best-effort; swallow notification failures */
  }
}

export function useVoteBrowserNotifications() {
  return {
    unsupported,
    permission,
    dismissed,
    showCta,
    canNotify,
    requestPermission,
    dismissCta,
    notifyVote,
  }
}