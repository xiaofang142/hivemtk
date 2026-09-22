import { reactive, readonly } from 'vue'

const state = reactive({
  visible: false,
  type: 'info',
  message: '',
  duration: 2400,
})

let timer = null

function show(type, message, duration = 2400) {
  state.type = type
  state.message = String(message ?? '')
  state.duration = duration
  state.visible = true
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => {
    state.visible = false
  }, duration)
}

export function useToast() {
  return {
    state: readonly(state),
    show,
    info: (msg, dur) => show('info', msg, dur),
    success: (msg, dur) => show('success', msg, dur),
    warning: (msg, dur) => show('warning', msg, dur),
    error: (msg, dur) => show('error', msg, dur),
  }
}

