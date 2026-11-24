import { http } from '@/utils/request'

export const clueApi = {
  delete: (id) => {
    return http.delete(`/api/clues/delete/${id}`)
  },
  list: (page, limit, filters = {}) => {
    const q = new URLSearchParams({ page: String(page), limit: String(limit) })
    Object.entries(filters).forEach(([key, value]) => {
      if (value !== '' && value !== null && value !== undefined) q.set(key, String(value))
    })
    return http.get(`/api/clues/list?${q}`)
  },
  statistics: () => {
    return http.get(`/api/clues/statistics`)
  },
  // 区间统计：服务端按 create_time 聚合角色/状态/渠道/单群聊六维，
  // 替代前端拉列表自行聚合时的 10000 行静默截断。
  statisticsRange: (start, end) => {
    const q = new URLSearchParams()
    if (start) q.set('start', String(start))
    if (end) q.set('end', String(end))
    return http.get(`/api/clues/statistics/range?${q}`)
  },
  import: (data) => {
    return http.post(`/api/clues/import`,data)
  },
  // 单条编辑/改派（UWA-23）：PUT /api/clues/:id，指针语义 null=不改
  update: (id, data) => {
    return http.put(`/api/clues/${id}`, data)
  },
}