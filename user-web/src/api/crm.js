import { http } from '@/utils/request'

const base = '/api/manage/crm'

// US1 客户统一主数据接口（后端 T028/T029）。
export const crmApi = {
  listCustomers(params = {}) {
    return http.get(`${base}/customers`, params)
  },
  getCustomer(id) {
    return http.get(`${base}/customers/${id}`)
  },
  createCustomer(data) {
    return http.post(`${base}/customers`, data)
  },
  updateCustomer(id, data) {
    return http.put(`${base}/customers/${id}`, data)
  },
  deleteCustomer(id) {
    return http.delete(`${base}/customers/${id}`)
  },
  scanDedupe(data = {}) {
    return http.post(`${base}/dedupe/scan`, data)
  },
  merge(data) {
    return http.post(`${base}/merge`, data)
  },
  mergeAudit(customerId) {
    return http.get(`${base}/merge/audit`, { customer_id: customerId })
  },
  importRows(data) {
    return http.post(`${base}/import`, data)
  },
  exportRows(params = {}) {
    return http.get(`${base}/export`, params)
  },
  view360(id) {
    return http.get(`${base}/360/${id}`)
  },
  crossIndustry(params = {}) {
    return http.get(`${base}/cross-industry`, params)
  },
  aiAnalyze(data) {
    return http.post(`${base}/ai/analyze`, data)
  },
  createContact(data) {
    return http.post(`${base}/contacts`, data)
  },
  createSupplier(data) {
    return http.post(`${base}/suppliers`, data)
  },
  createServiceProvider(data) {
    return http.post(`${base}/service-providers`, data)
  },
  archiveClue(data) {
    return http.post(`${base}/clue/archive`, data)
  },
}

export default crmApi
