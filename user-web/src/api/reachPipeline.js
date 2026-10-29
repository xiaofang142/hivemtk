import { http } from '@/utils/request'

export const reachPipelineApi = {
  getPipelines(params) {
    return http.get('/api/reach/pipelines', params)
  },
  createPipeline(data) {
    return http.post('/api/reach/pipelines', data)
  },
  getPipeline(id) {
    return http.get(`/api/reach/pipelines/${id}`)
  },
  updatePipeline(id, data) {
    return http.put(`/api/reach/pipelines/${id}`, data)
  },
  deletePipeline(id) {
    return http.delete(`/api/reach/pipelines/${id}`)
  },
  pausePipeline(id) {
    return http.post(`/api/reach/pipelines/${id}/pause`, {})
  },
  resumePipeline(id) {
    return http.post(`/api/reach/pipelines/${id}/resume`, {})
  },
  archivePipeline(id) {
    return http.post(`/api/reach/pipelines/${id}/archive`, {})
  },

  getJobs(params) {
    return http.get('/api/reach/jobs', params)
  },
  enqueueJob(data) {
    return http.post('/api/reach/jobs', data)
  },
  getJob(id) {
    return http.get(`/api/reach/jobs/${id}`)
  },
  cancelJob(id) {
    return http.post(`/api/reach/jobs/${id}/cancel`, {})
  },
  retryJob(id) {
    return http.post(`/api/reach/jobs/${id}/retry`, {})
  },
  executeJob(id) {
    return http.post(`/api/reach/jobs/${id}/execute`, {})
  },

  getStats(params) {
    return http.get('/api/reach/stats', params)
  },
  resetRateLimit(channel) {
    return http.post('/api/reach/rate-limit/reset', {}, { params: { channel } })
  },

  // D02 延迟出站（静默时段 / 合规窗口）：查询 + 取消
  // GET /api/reach/deferred-outbound?status=&channel=&page=&page_size=
  getDeferredOutbounds(params) {
    return http.get('/api/reach/deferred-outbound', params)
  },
  // POST /api/reach/deferred-outbound/:id/cancel（仅 pending 可取消）
  // _silent：404 表示"已不在 pending、不可取消"，由页面给出更具体的提示。
  cancelDeferredOutbound(id) {
    return http.post(`/api/reach/deferred-outbound/${id}/cancel`, {}, { _silent: true })
  }
};
