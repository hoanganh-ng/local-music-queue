<template>
  <Teleport to="body">
    <div v-if="request" class="dialog-backdrop" @click.self="cancel">
      <section
        ref="dialogRef"
        class="confirm-dialog glass-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        aria-describedby="confirm-message"
        tabindex="-1"
        @keydown.esc.prevent="cancel"
      >
        <h2 id="confirm-title">{{ request.title }}</h2>
        <p id="confirm-message">{{ request.message }}</p>
        <div class="dialog-actions">
          <BaseButton variant="secondary" @click="cancel">{{ request.cancelLabel }}</BaseButton>
          <BaseButton :variant="request.danger ? 'danger' : 'primary'" @click="accept">
            {{ request.confirmLabel }}
          </BaseButton>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<script setup>
import { nextTick, ref, watch } from 'vue'
import { useConfirm } from '../../composables/useConfirm'
import BaseButton from './BaseButton.vue'

const { request, resolveConfirm } = useConfirm()
const dialogRef = ref(null)
let lastFocused = null

watch(request, async (newRequest) => {
  if (newRequest) {
    lastFocused = document.activeElement
    await nextTick()
    dialogRef.value?.focus()
  } else if (lastFocused?.focus) {
    lastFocused.focus()
    lastFocused = null
  }
})

function accept() {
  resolveConfirm(true)
}

function cancel() {
  resolveConfirm(false)
}
</script>

<style scoped>
.dialog-backdrop {
  position: fixed;
  inset: 0;
  z-index: 999;
  display: grid;
  place-items: center;
  padding: 1rem;
  background: rgba(0, 0, 0, 0.58);
}

.confirm-dialog {
  width: min(420px, 100%);
  padding: 1.5rem;
}

.confirm-dialog h2 {
  margin: 0 0 0.75rem;
  color: var(--text-main);
  font-size: 1.25rem;
}

.confirm-dialog p {
  color: var(--text-muted);
  line-height: 1.5;
}

.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.75rem;
  margin-top: 1.5rem;
}

@media (max-width: 480px) {
  .dialog-actions {
    flex-direction: column-reverse;
  }
}
</style>
