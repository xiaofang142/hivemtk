import { http } from '@/utils/request'

// 渠道识别码管理（US4 / FR-028）：一次性替换旧 Bridge token。
// 列表/生成/重置统一走后端 /api/identify-codes（预设渠道表驱动）。
export const channelIdentifyCodeApi = {
  list(params = {}) {
    return http.get('/api/identify-codes', params)
  },
  get(channelId) {
    return http.get(`/api/identify-codes/${channelId}`)
  },
  // 为渠道生成识别码；已存在则重置（version+1，旧码进入短暂在途窗口）。
  rotate(channelId) {
    return http.post(`/api/identify-codes/${channelId}/rotate`)
  }
}

export default channelIdentifyCodeApi
