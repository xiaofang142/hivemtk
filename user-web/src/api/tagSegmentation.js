import { http } from '@/utils/request'

export const TagSegmentationApi = {
  getTags: (params) => {
    return http.get('/api/session-tags', params)
  },

  updateTags: (data) => {
    // 服务端只有 PUT /api/session-tags/:id（service_routes.go:67）：打到集合路径上是 405，
    // 编辑标签从来存不下去。id 走路径， body 里那份摘掉，避免多带一个字段。
    const { id, ...body } = data
    return http.put(`/api/session-tags/${id}`, body)
  },

  createTag: (data) => {
    return http.post('/api/session-tags', data)
  },

  deleteTag: (id) => {
    return http.delete(`/api/session-tags/${id}`)
  },

  getTagRules: (params) => {
    return http.get('/api/customer-360/tag-rules', params)
  },

  saveTagRule: (data) => {
    return http.post('/api/customer-360/tag-rules', data)
  },

  updateTagRule: (id, data) => {
    return http.put(`/api/customer-360/tag-rules/${id}`, data)
  },

  deleteTagRule: (id) => {
    return http.delete(`/api/customer-360/tag-rules/${id}`)
  },

  getLayerStrategy: () => {
    return http.get('/api/user-segment/layers')
  },

  getTagStats: (params) => {
    return http.get('/api/customer-360/tag-stats', params)
  }
};
