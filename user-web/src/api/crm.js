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
  listContacts(customerId, params = {}) {
    return http.get(`${base}/contacts`, { customer_id: customerId, ...params })
  },
  createContact(data) {
    return http.post(`${base}/contacts`, data)
  },
  updateContact(id, data) {
    return http.put(`${base}/contacts/${id}`, data)
  },
  deleteContact(id) {
    return http.delete(`${base}/contacts/${id}`)
  },
  listSuppliers(customerId, params = {}) {
    return http.get(`${base}/suppliers`, { customer_id: customerId, ...params })
  },
  createSupplier(data) {
    return http.post(`${base}/suppliers`, data)
  },
  updateSupplier(id, data) {
    return http.put(`${base}/suppliers/${id}`, data)
  },
  deleteSupplier(id) {
    return http.delete(`${base}/suppliers/${id}`)
  },
  listServiceProviders(customerId, params = {}) {
    return http.get(`${base}/service-providers`, { customer_id: customerId, ...params })
  },
  createServiceProvider(data) {
    return http.post(`${base}/service-providers`, data)
  },
  updateServiceProvider(id, data) {
    return http.put(`${base}/service-providers/${id}`, data)
  },
  deleteServiceProvider(id) {
    return http.delete(`${base}/service-providers/${id}`)
  },
  archiveClue(data) {
    return http.post(`${base}/clue/archive`, data)
  },
}

export default crmApi
