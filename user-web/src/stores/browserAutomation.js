import { defineStore } from 'pinia'
import { ref } from 'vue'
import { runBrowserTask, getBrowserSessionSteps, stopBrowserSession } from '@/api/browserAutomation'

// 拦截器已拆 data.data，页面拿到的是业务数据本身；res?.data || res 兜底
const unpack = (res) => res?.data ?? res

export const useBrowserAutomationStore = defineStore('browserAutomation', () => {
  const runningSession = ref(null)
  const steps = ref([])
  let pollTimer = null

  const stopPoll = () => {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  async function pollSteps(sessionId) {
    try {
      const res = await getBrowserSessionSteps(sessionId)
      const list = unpack(res)
      steps.value = Array.isArray(list) ? list : list?.list || []
      const allDone = steps.value.length > 0 && steps.value.every((s) =>
        ['success', 'failed', 'skipped', 'pending'].includes(s.status) === false || s.status !== 'running'
      )
      const sessionRes = await import('@/api/browserAutomation').then((m) => m.getBrowserSession(sessionId))
      const session = unpack(sessionRes)
      if (session && ['completed', 'failed', 'stopped'].includes(session.status)) {
        runningSession.value = session
        stopPoll()
      } else if (allDone && steps.value.every((s) => s.status !== 'running')) {
        // steps 全部终态但 session 未收口：再等下一轮（由 session 状态兜底）
      }
    } catch {
      // 轮询失败静默（网络抖动），由 session 超时兜底
    }
  }

  async function startRun(taskId) {
    const res = await runBrowserTask(taskId)
    const data = unpack(res)
    runningSession.value = { id: data?.session_id, status: data?.status }
    steps.value = []
    stopPoll()
    if (data?.session_id) {
      pollTimer = setInterval(() => pollSteps(data.session_id), 2000)
    }
    return data
  }

  async function stopSession(sessionId) {
    stopPoll()
    return stopBrowserSession(sessionId, '用户手动中断')
  }

  return { runningSession, steps, startRun, stopPoll, stopSession }
})
