import { http } from '@/utils/request'

export function getSystemStatus() {
  return http.get('/api/system/stats')
}

export function getOpsStats() {
  return http.get('/api/monitor/health')
}

export function getModuleStatus() {
  return http.get('/api/monitor/node-health')
}

export function getRecentOperations(limit = 20) {
  return http.get('/api/monitor/anomalies', { limit })
}

/**
 * Get sales cockpit — 聚合 ReAct / SOP / RAG / 触达 / LLM 路由 / 渠道健康 / Top 工具
 * GET /api/ai/sales-cockpit
 */
export function getSalesCockpit() {
  return http.get('/api/ai/sales-cockpit')
}
