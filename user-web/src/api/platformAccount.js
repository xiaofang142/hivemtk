import { http } from '@/utils/request'

export const platformAccountApi = {
  getAccounts(params) {
    return http.get('/api/platform-accounts', { params })
  },

  getPlatforms() {
    return http.get('/api/platform-accounts/platforms')
  },

  getAccountById(id) {
    return http.get(`/api/platform-accounts/${id}`)
  },

  createAccount(data) {
    return http.post('/api/platform-accounts', data)
  },

  updateAccount(id, data) {
    return http.put(`/api/platform-accounts/${id}`, data)
  },

  deleteAccount(id) {
    return http.delete(`/api/platform-accounts/${id}`)
  },

  loginAccount(id, data) {
    return http.post(`/api/platform-accounts/${id}/login`, data)
  },

  checkStatus(id) {
    // 该平台不支持服务端登录态检查时后端返回 400（能力已下线），
    // 由弹窗内联提示承载，不再叠加全局 toast。
    return http.get(`/api/platform-accounts/${id}/status`, undefined, { _silent: true })
  }
};
