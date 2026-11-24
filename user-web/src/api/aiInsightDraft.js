import { http } from '@/utils/request'

const base = '/api/ai-insights/drafts'
const seg = (id) => encodeURIComponent(id)

export const aiInsightDraftApi = {
  list(params = {}) {
    return http.get(base, params)
  },
  get(id) {
    return http.get(`${base}/${seg(id)}`)
  },
  create(data) {
    return http.post(base, data)
  },
  generate(data) {
    return http.post(`${base}/generate`, data)
  },
  confirm(id, data = {}) {
    return http.post(`${base}/${seg(id)}/confirm`, data)
  },
  reject(id, data = {}) {
    return http.post(`${base}/${seg(id)}/reject`, data)
  },
}

export default aiInsightDraftApi
