import { http } from '@/utils/request';

export function getOperationLogs(params) {
  return http.get('/api/operation-logs', params)
}
export function getOperationLogDetail(id) {
  return http.get(`/api/operation-logs/${id}`)
}
export function getOperationLogStatistics(params) {
  return http.get('/api/operation-logs/statistics', params)
}
export function exportOperationLogs(params) {
  return http.get('/api/operation-logs/export', params, { responseType: 'blob' });
}
export function cleanOperationLogs(beforeDate) {
  // 后端 binding 字段是 before_date（snake_case）：传 beforeDate 会被静默忽略，
  // 清理退回默认 90 天窗口——破坏性接口不能靠"没报错"假设参数生效。
  return http.post('/api/operation-logs/clean', { before_date: beforeDate })
}
