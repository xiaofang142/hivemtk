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
  return http.post('/api/operation-logs/clean', { beforeDate })
}
