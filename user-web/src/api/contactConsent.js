import { http } from '@/utils/request'

// D04 客户同意时间线 / 偏好
//
// 同意事实账只追加（grant/revoke），actor 由后端从登录上下文取，前端不传。
const base = (customerId) => `/api/contacts/${encodeURIComponent(customerId)}`

export const contactConsentApi = {
  // GET /api/contacts/:customer_id/consents?limit=100
  getTimeline(customerId, params) {
    return http.get(`${base(customerId)}/consents`, params)
  },
  // POST /api/contacts/:customer_id/consents
  record(customerId, data) {
    return http.post(`${base(customerId)}/consents`, data)
  },
  // GET /api/contacts/:customer_id/preferences
  getPreferences(customerId) {
    return http.get(`${base(customerId)}/preferences`)
  },
  // PUT /api/contacts/:customer_id/preferences
  setPreference(customerId, data) {
    return http.put(`${base(customerId)}/preferences`, data)
  }
}
