import { http } from '@/utils/request'

export const usersApi = {
  list: (params) => {
    return http.get('/api/users', { params })
  },
  
  get: (id) => {
    return http.get(`/api/users/${id}`)
  },
  
  create: (data) => {
    return http.post('/api/users', data)
  },
  
  update: (id, data) => {
    return http.put(`/api/users/${id}`, data)
  },
  
  delete: (id) => {
    return http.delete(`/api/users/${id}`)
  },
  
  updatePassword: (id, data) => {
    return http.put(`/api/users/${id}/password`, data)
  },
  
  login: (data) => {
    return http.post('/api/auth/login', data)
  },

  // MFA 登录第二步：公开端点（无 JWT），凭 temp_token + 6 位 TOTP/恢复码换取终令牌
  mfaVerify: (data) => {
    return http.post('/api/auth/mfa/verify', data)
  },

  // 登出：后端把当前令牌拉黑并撤销 HttpOnly Cookie（Cookie 通道由服务端从请求里
  // 回落读取，前端内存里没有令牌值也一样能登出）。之后前端只需清内存态。
  logout: () => {
    return http.post('/api/auth/logout')
  }
}