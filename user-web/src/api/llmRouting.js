import { http } from '@/utils/request'

export const LlmRoutingApi = {
  // 按日聚合（P1-4）：window=today/week/month/all，groupBy=day/day_scenario/day_provider。
  // 后端 /api/llm/usage/daily 早已存在，此前后台页面没有任何消费方（数据面空转）。
  getDailyUsage: (window, groupBy) => {
    return http.get('/api/llm/usage/daily', { window, group_by: groupBy })
  },

  getModelList: (params) => {
    return http.get('/api/llm/models', params)
  },

  getModelDetail: (name) => {
    return http.get(`/api/llm/models/${encodeURIComponent(name)}`)
  },

  createModel: (data) => {
    return http.post('/api/llm/models', data)
  },

  updateModel: (name, data) => {
    return http.put(`/api/llm/models/${encodeURIComponent(name)}`, data)
  },

  deleteModel: (name) => {
    return http.delete(`/api/llm/models/${encodeURIComponent(name)}`)
  },

  updateModelStatus: (name, status) => {
    return http.put(`/api/llm/models/${encodeURIComponent(name)}`, { enabled: status })
  },

  testModel: (name, body = {}) => {
    return http.post(`/api/llm/models/${encodeURIComponent(name)}/test`, body)
  },

  getSceneRouting: () => {
    return http.get('/api/llm/scene-routing')
  },

  getScenarios: () => {
    return http.get('/api/llm/scenarios')
  },

  saveSceneRouting: (data) => {
    const payload = Array.isArray(data) ? { routes: data } : data
    return http.put('/api/llm/scene-routing', payload)
  },

  getFallbackStrategy: () => {
    return http.get('/api/llm/fallback')
  },

  getAuditHistory: (scenario, limit = 50) => {
    const params = { limit }
    if (scenario) params.scenario = scenario
    return http.get('/api/llm/audit', params)
  },

  getStats: () => {
    return http.get('/api/llm/stats')
  },

  getUsage: (window = 'all') => {
    return http.get('/api/llm/usage', { window })
  },

  // 平台垫资用量明细（PLT-01 刀2）：GET /api/llm/platform-usage，分页参数
  // { page, page_size }，行=既有记录列（成本三列为写入时点整数分快照）。
  getPlatformUsage: (params) => {
    return http.get('/api/llm/platform-usage', params)
  },

  getCostStats: (window = 'month') => {
    return http.get('/api/llm/cost-stats', { window })
  },

  getHealth: () => {
    return http.get('/api/llm/health')
  },

  getModelTypeStats: (window = 'month') => {
    return http.get('/api/llm/model-type-stats', { window })
  },

  getEgressAudit: (params = {}) => {
    return http.get('/api/llm/egress-audit', params)
  },

  getEgressAlerts: (params = {}) => {
    return http.get('/api/llm/egress-alerts', params)
  }
};
