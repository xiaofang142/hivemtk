import { http } from '@/utils/request';

export function listAgents(params) {
  return http.get('/api/ai-agents', params)
}

export function listEnabledAgents() {
  return http.get('/api/ai-agents-enabled')
}

export function getAgent(id) {
  return http.get(`/api/ai-agents/${id}`)
}

export function findAgentByCode(code) {
  return http.get('/api/ai-agents/by-code', { agent_code: code })
}

export function createAgent(data) {
  return http.post('/api/ai-agents', data)
}

export function updateAgent(id, data) {
  return http.put(`/api/ai-agents/${id}`, data)
}

export function deleteAgent(id) {
  return http.delete(`/api/ai-agents/${id}`)
}

export function toggleAgent(id, status) {
  return http.post(`/api/ai-agents/${id}/toggle`, { status })
}

export function testAgent(id, data) {
  return http.post(`/api/ai-agents/${id}/test`, data)
}

export function getAgentContext(id) {
  return http.get(`/api/ai-agents/${id}/context`)
}
