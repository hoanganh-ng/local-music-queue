import { readonly, ref } from 'vue'

const toasts = ref([])
let nextId = 1

function pushToast(message, type = 'info', timeout = 4000) {
  const id = nextId++
  toasts.value.push({ id, message, type })

  if (timeout > 0) {
    setTimeout(() => removeToast(id), timeout)
  }

  return id
}

function removeToast(id) {
  toasts.value = toasts.value.filter((toast) => toast.id !== id)
}

export function useToast() {
  return {
    toasts: readonly(toasts),
    success: (message, timeout) => pushToast(message, 'success', timeout),
    error: (message, timeout) => pushToast(message, 'error', timeout),
    info: (message, timeout) => pushToast(message, 'info', timeout),
    removeToast
  }
}
