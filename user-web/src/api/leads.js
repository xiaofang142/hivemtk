import { http } from '@/utils/request'

const base = '/api/manage/leads'

// US2 线索模块接口（后端 T044：规则 / 通知路由 / 判定证据 / 归档 / 权限内列表）。
export const leadsApi = {
  listRules(params = {}) {
    return http.get(`${base}/rules`, params)
  },
  saveRule(data) {
    return http.post(`${base}/rules`, data)
  },
  deleteRule(id) {
    return http.delete(`${base}/rules/${id}`)
  },
  listNotifyRoutes(params = {}) {
    return http.get(`${base}/notify-routes`, params)
  },
  createNotifyRoute(data) {
    return http.post(`${base}/notify-routes`, data)
  },
  deleteNotifyRoute(routeId) {
    return http.delete(`${base}/notify-routes/${routeId}`)
  },
  listScopedMessages(params = {}) {
    return http.get(`${base}/messages`, params)
  },
  listScopedCustomers(params = {}) {
    return http.get(`${base}/customers`, params)
  },
  judgementHits(judgementId) {
    return http.get(`${base}/judgements/${judgementId}/hits`)
  },
  archiveClue(data) {
    return http.post(`${base}/archive`, data)
  }
}

export default leadsApi
