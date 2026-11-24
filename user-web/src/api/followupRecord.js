import { http } from '@/utils/request'

const base = '/api/followup-records'
const seg = (id) => encodeURIComponent(id)

export const followupRecordApi = {
  list(params = {}) {
    return http.get(base, params)
  },
  get(id) {
    return http.get(`${base}/${seg(id)}`)
  },
  create(data) {
    return http.post(base, data)
  },
  update(id, data) {
    return http.put(`${base}/${seg(id)}`, data)
  },
}

export default followupRecordApi
