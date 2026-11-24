import { http } from '@/utils/request'

const base = '/api/business-account-auths'
const seg = (id) => encodeURIComponent(id)

export const businessAccountAuthApi = {
  list(params = {}) {
    return http.get(base, params)
  },
  upsert(data) {
    return http.post(base, data)
  },
  remove(id) {
    return http.delete(`${base}/${seg(id)}`)
  },
}

export default businessAccountAuthApi
