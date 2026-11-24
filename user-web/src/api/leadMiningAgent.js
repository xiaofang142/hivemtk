import { http } from '@/utils/request'

const base = '/api/manage/lead-mining-agents'

// 线索发现智能体独立注册管理（FR-026）：与自动回复智能体分开维护。
export const leadMiningAgentApi = {
  listAgents(params = {}) {
    return http.get(base, params)
  },
  getAgent(id) {
    return http.get(`${base}/${id}`)
  },
  createAgent(data) {
    return http.post(base, data)
  },
  updateAgent(id, data) {
    return http.put(`${base}/${id}`, data)
  },
  deleteAgent(id) {
    return http.delete(`${base}/${id}`)
  }
}

export default leadMiningAgentApi
