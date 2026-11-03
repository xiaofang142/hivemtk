import { http } from '@/utils/request';

export function getMarketingFlowList(params) {
  return http.get('/api/marketing-flows', params)
}
export function getMarketingFlow(id) {
  return http.get(`/api/marketing-flows/${id}`)
}
export function createMarketingFlow(data) {
  return http.post('/api/marketing-flows', data)
}
export function updateMarketingFlow(id, data) {
  return http.put(`/api/marketing-flows/${id}`, data)
}
export function deleteMarketingFlow(id) {
  return http.delete(`/api/marketing-flows/${id}`)
}
export function activateFlow(id) {
  return http.post(`/api/marketing-flows/${id}/activate`)
}
export function pauseMarketingFlow(id) {
  return http.post(`/api/marketing-flows/${id}/pause`)
}
export function stopMarketingFlow(id) {
  return http.post(`/api/marketing-flows/${id}/stop`)
}
export function getFlowExecutions(id) {
  return http.get(`/api/marketing-flows/${id}/executions`)
}
export function getFlowStats(id) {
  return http.get(`/api/marketing-flows/${id}/stats`)
}

export function getFlows(params) {
  return getMarketingFlowList(params)
}
export function createFlow(data) {
  return createMarketingFlow(data)
}
export function updateFlow(id, data) {
  return updateMarketingFlow(id, data)
}
export function deleteFlow(id) {
  return deleteMarketingFlow(id)
}
// 执行 = 真正跑一次流程（POST /:id/trigger），而不是把状态改成 activate
export function triggerFlow(id) {
  return http.post(`/api/marketing-flows/${id}/trigger`, {})
}
export function runFlow(id) {
  return triggerFlow(id)
}
export function toggleFlow(id, enabled) {
  return enabled ? activateFlow(id) : pauseMarketingFlow(id)
}
export function getFlowLogs(id) {
  return getFlowExecutions(id)
}
