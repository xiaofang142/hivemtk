import { http } from '@/utils/http'

// ========== 任务 ==========

export const listBrowserTasks = (params) =>
  http.get('/api/browser-automation/tasks', params)

export const getBrowserTask = (id) =>
  http.get(`/api/browser-automation/tasks/${id}`)

export const createBrowserTask = (data) =>
  http.post('/api/browser-automation/tasks', data)

export const updateBrowserTask = (id, data) =>
  http.put(`/api/browser-automation/tasks/${id}`, data)

export const deleteBrowserTask = (id) =>
  http.delete(`/api/browser-automation/tasks/${id}`)

export const publishBrowserTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/publish`)

// 异步执行：立即返回 {session_id, status}
// _silent：409 在域内有三种结论（Host 未连接 / 已有任务占用 / 依赖未满足），
// 该开引导弹窗还是提示条由列表页按 bizCode 分流，拦截器再弹一次会让「占用」出现两条同文案。
export const runBrowserTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/run`, {}, { _silent: true })

export const pauseBrowserTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/pause`)

export const resumeBrowserTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/resume`)

export const archiveBrowserTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/archive`)

export const setBrowserTaskDependency = (id, data) =>
  http.put(`/api/browser-automation/tasks/${id}/dependency`, data)

// ========== Session ==========

export const listBrowserSessions = (params) =>
  http.get('/api/browser-automation/sessions', params)

export const getBrowserSession = (id) =>
  http.get(`/api/browser-automation/sessions/${id}`)

export const getBrowserSessionSteps = (id) =>
  http.get(`/api/browser-automation/sessions/${id}/steps`)

// 注意命名：按任务查会话（避免与 listBrowserSessions 重名）
export const listBrowserTaskSessions = (taskId, params) =>
  http.get(`/api/browser-automation/tasks/${taskId}/sessions`, params)

export const stopBrowserSession = (id, reason) =>
  http.post(`/api/browser-automation/sessions/${id}/stop`, { reason: reason || '' })

// D7：放行 require_confirm 闸门上挂起的写操作提交点（会话详情 confirm_pending=true 时可调用）
export const confirmBrowserSession = (id) =>
  http.post(`/api/browser-automation/sessions/${id}/confirm`, {})

// D1（G1 补口）：append-only 命令流审计（direction 可选 command/event/judge）
export const getBrowserSessionLogs = (id, direction) =>
  http.get(`/api/browser-automation/sessions/${id}/logs`, direction ? { direction } : undefined)

// I5：审计包导出（会话+步流水+命令流+LLM 成本账单请求归并，前端落盘 JSON）
export const exportBrowserSessionAudit = (id) =>
  http.get(`/api/browser-automation/sessions/${id}/export`)

// ========== Cron ==========

export const listBrowserCron = () =>
  http.get('/api/browser-automation/cron')

export const createBrowserCron = (data) =>
  http.post('/api/browser-automation/cron', data)

export const updateBrowserCron = (id, data) =>
  http.put(`/api/browser-automation/cron/${id}`, data)

export const deleteBrowserCron = (id) =>
  http.delete(`/api/browser-automation/cron/${id}`)

export const enableBrowserCron = (id) =>
  http.post(`/api/browser-automation/cron/${id}/enable`)

export const disableBrowserCron = (id) =>
  http.post(`/api/browser-automation/cron/${id}/disable`)

// ========== Host（admin）==========

export const getBrowserHostStatus = () =>
  http.get('/api/browser-automation/host/status')

export const resetBrowserHostToken = () =>
  http.post('/api/browser-automation/host/token/reset')

// ========== 平台（L3 注册表）==========

export const listPlatforms = () =>
  http.get('/api/browser-automation/platforms')

export const getPlatformLocators = (id) =>
  http.get(`/api/browser-automation/platforms/${id}/locators`)
