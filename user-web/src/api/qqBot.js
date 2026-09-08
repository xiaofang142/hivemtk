import { http } from '@/utils/request';

export function listAccounts(params = {}) {
  return http.get('/api/qq/accounts', params)
}

export function getAccount(id) {
  return http.get(`/api/qq/accounts/${id}`)
}

export function createAccount(data) {
  return http.post('/api/qq/accounts', data)
}

export function updateAccount(id, data) {
  return http.put(`/api/qq/accounts/${id}`, data)
}

export function deleteAccount(id) {
  return http.delete(`/api/qq/accounts/${id}`)
}

export function testSend(id, data) {
  return http.post(`/api/qq/accounts/${id}/test-send`, data)
}
