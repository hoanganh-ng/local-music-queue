import { readonly, ref } from 'vue'

const request = ref(null)

export function useConfirm() {
  function confirm(options) {
    return new Promise((resolve) => {
      request.value = {
        title: options.title || 'Confirm action',
        message: options.message || 'Are you sure?',
        confirmLabel: options.confirmLabel || 'Confirm',
        cancelLabel: options.cancelLabel || 'Cancel',
        danger: options.danger || false,
        resolve
      }
    })
  }

  function resolveConfirm(value) {
    if (!request.value) return
    request.value.resolve(value)
    request.value = null
  }

  return {
    request: readonly(request),
    confirm,
    resolveConfirm
  }
}
