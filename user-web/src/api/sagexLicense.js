import { http } from '@/utils/request';

export function listSageXCodes(params) {
  return http.get('/api/sagex/licenses', params)
}

export function issueSageXCode() {
  return http.post('/api/sagex/licenses/issue')
}

export function invalidateSageXCode(code, reason) {
  return http.post(`/api/sagex/licenses/${code}/invalidate`, { reason })
}

export function listSageXHeartbeats(code) {
  return http.get('/api/sagex/licenses/heartbeats', { code })
}