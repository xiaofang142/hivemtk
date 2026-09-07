import { http } from '@/utils/request';

export function listAccounts(params = {}) {
  return http.get('/api/telegram/accounts', params)
}

export function getAccount(id) {
  return http.get(`/api/telegram/accounts/${id}`)
}

export function createAccount(data) {
  return http.post('/api/telegram/accounts', data)
}

export function updateAccount(id, data) {
  return http.put(`/api/telegram/accounts/${id}`, data)
}

export function deleteAccount(id) {
  return http.delete(`/api/telegram/accounts/${id}`)
}

export function registerWebhook(id, data = {}) {
  return http.post(`/api/telegram/accounts/${id}/register-webhook`, data)
}

export function testSend(id, data) {
  return http.post(`/api/telegram/accounts/${id}/test-send`, data)
}

// ========== 入群管控网关（方案 A 申请审批 / 方案 B 禁言解锁） ==========

export function listGates(params = {}) {
  return http.get('/api/telegram/gates', params)
}

export function createGate(data) {
  return http.post('/api/telegram/gates', data)
}

export function updateGate(id, data) {
  return http.put(`/api/telegram/gates/${id}`, data)
}

export function deleteGate(id) {
  return http.delete(`/api/telegram/gates/${id}`)
}

export function listGateMembers(id, params = {}) {
  return http.get(`/api/telegram/gates/${id}/members`, params)
}

export function authorizeGateMember(id, memberId) {
  return http.post(`/api/telegram/gates/${id}/members/${memberId}/authorize`)
}
