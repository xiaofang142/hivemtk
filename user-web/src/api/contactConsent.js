import { http } from '@/utils/request'

// D04 客户同意时间线 / 偏好
//
// 同意事实账只追加（grant/revoke），actor 由后端从登录上下文取，前端不传。
// 路径字面量内联（不抽 base 帮助函数）：audit_api_contract.py 的静态解析
// 只认「模板字符串里 ${} 是路径参数」，整段 base 走箭头函数会被拆成 :param
// 丢掉 /api/contacts 前缀，导致 4 条假 UNMATCHED 卡 strict 门。
export const contactConsentApi = {
  // GET /api/contacts/:customer_id/consents?limit=100
  getTimeline(customerId, params) {
    return http.get(`/api/contacts/${encodeURIComponent(customerId)}/consents`, params)
  },
  // POST /api/contacts/:customer_id/consents
  record(customerId, data) {
    return http.post(`/api/contacts/${encodeURIComponent(customerId)}/consents`, data)
  },
  // GET /api/contacts/:customer_id/preferences
  getPreferences(customerId) {
    return http.get(`/api/contacts/${encodeURIComponent(customerId)}/preferences`)
  },
  // PUT /api/contacts/:customer_id/preferences
  setPreference(customerId, data) {
    return http.put(`/api/contacts/${encodeURIComponent(customerId)}/preferences`, data)
  }
}
