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
export const runBrowserTask = (id) =>
  http.post(`/api/browser-automation/tasks/${id}/run`)

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
