import { http } from '@/utils/request'

const base = '/api/customer-import-jobs'
const seg = (id) => encodeURIComponent(id)

export const customerImportJobApi = {
  list(params = {}) {
    return http.get(base, params)
  },
  get(id) {
    return http.get(`${base}/${seg(id)}`)
  },
  template(entityType = 'customer') {
    return http.get(`${base}/template`, { entity_type: entityType }, { responseType: 'blob' })
  },
  precheck(formData) {
    return http.upload(`${base}/precheck`, formData)
  },
  execute(id) {
    return http.post(`${base}/${seg(id)}/execute`)
  },
  errors(id) {
    return http.get(`${base}/${seg(id)}/errors`)
  },
  downloadErrors(id) {
    return http.get(`${base}/${seg(id)}/errors/download`, {}, { responseType: 'blob' })
  },
  create(data) {
    return http.post(base, data)
  },
}

export default customerImportJobApi
