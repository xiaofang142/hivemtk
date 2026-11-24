import { http } from '@/utils/request'

const base = '/api/companies'

// 公司主数据：一客户可关联多家公司，列表与关系都由既有 CompanyController 提供。
export const companyApi = {
  listCompanies(params = {}) {
    return http.get(base, params)
  },
  getCompany(id) {
    return http.get(`${base}/${id}`)
  },
  createCompany(data) {
    return http.post(base, data)
  },
  updateCompany(id, data) {
    return http.put(`${base}/${id}`, data)
  },
  deleteCompany(id) {
    return http.delete(`${base}/${id}`)
  },
  listCompanyRelations(companyId) {
    return http.get(`${base}/${companyId}/relations`)
  },
  listCustomerRelations(customerId) {
    return http.get(`/api/customers/${customerId}/company-relations`)
  },
  upsertCustomerRelation(customerId, data) {
    return http.post(`/api/customers/${customerId}/company-relations`, data)
  }
}

export default companyApi
