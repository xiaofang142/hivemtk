import { http } from '@/utils/request'

// GEO 审计中心。site/content 两个入口共用 run/item 历史与 compare。
export const runSiteAudit = (data) => http.post('/api/geo/audit/site', data)

export const runContentAudit = (data) => http.post('/api/geo/audit/content', data)

export const listAuditRuns = (params = {}) => http.get('/api/geo/audit/runs', params)

export const getAuditRun = (id) => http.get(`/api/geo/audit/runs/${id}`)

export const compareAudits = (from, to) =>
  http.get('/api/geo/audit/compare', { from, to })

// 报告是二进制/HTML，走 blob 后由页面触发下载或新窗口打开。
export const fetchAuditReport = (id, format = 'html') =>
  http.get(`/api/geo/audit/runs/${id}/report`, { format }, { responseType: 'blob' })
