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
  import: (data) => {
    return http.post(`/api/clues/import`,data)
  },
  // 单条编辑/改派（UWA-23）：PUT /api/clues/:id，指针语义 null=不改
  update: (id, data) => {
    return http.put(`/api/clues/${id}`, data)
  },
}