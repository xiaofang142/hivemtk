<script setup>
import { useToast } from '../composables/useToast.js'

const { state } = useToast()
</script>

<template>
  <transition name="toast-fade">
    <div
      v-if="state.visible"
      :class="['toast', `toast-${state.type}`]"
      :role="state.type === 'error' ? 'alert' : 'status'"
      :aria-live="state.type === 'error' ? 'assertive' : 'polite'"
    >
      <span class="toast-icon" aria-hidden="true">
        <template v-if="state.type === 'success'">✓</template>
        <template v-else-if="state.type === 'warning'">!</template>
        <template v-else-if="state.type === 'error'">×</template>
        <template v-else>i</template>
      </span>
      <span class="toast-msg">{{ state.message }}</span>
    </div>
  </transition>
</template>

<style scoped>
.toast {
  position: fixed;
  top: 24px;
  left: 50%;
  transform: translateX(-50%);
  min-width: 200px;
  max-width: 80vw;
  padding: 12px 20px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  font-weight: 500;
  color: #fff;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
  z-index: 9999;
  pointer-events: none;
  user-select: none;
  backdrop-filter: blur(8px);
}

.toast-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  font-weight: 700;
  font-size: 13px;
  background: rgba(255, 255, 255, 0.22);
  flex-shrink: 0;
}

.toast-msg {
  flex: 1;
  word-break: break-all;
}

.toast-info {
  background: linear-gradient(135deg, rgba(46, 124, 246, 0.95), rgba(76, 154, 255, 0.95));
}
.toast-success {
  background: linear-gradient(135deg, rgba(34, 197, 94, 0.95), rgba(74, 222, 128, 0.95));
}
.toast-warning {
  background: linear-gradient(135deg, rgba(245, 158, 11, 0.95), rgba(251, 191, 36, 0.95));
}
.toast-error {
  background: linear-gradient(135deg, rgba(239, 68, 68, 0.95), rgba(248, 113, 113, 0.95));
}

.toast-fade-enter-active,
.toast-fade-leave-active {
  transition: opacity 200ms ease, transform 200ms ease;
}
.toast-fade-enter-from,
.toast-fade-leave-to {
  opacity: 0;
  transform: translate(-50%, -10px);
}
</style>
