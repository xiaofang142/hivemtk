import { http } from '@/utils/request'

const base = '/api/cross-account-search'

export const crossAccountSearchApi = {
  searchCustomers(params = {}) {
    return http.get(`${base}/customers`, params)
  },
}

export default crossAccountSearchApi
