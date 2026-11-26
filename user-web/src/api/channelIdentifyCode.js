import { http } from '@/utils/request'

// 账号级识别码管理：每个渠道账号一枚唯一识别码（旧 Bridge token 已退役）。
// 列表由后端按账号聚合；生成/重置统一走账号级 rotate-for-account。
export const channelIdentifyCodeApi = {
  list(params = {}) {
    return http.get('/api/identify-codes', params)
  },
  // 账号级生成/重置（version 概念仅存量渠道码保留）；旧码进入短暂在途窗口。
  rotateForAccount(channelId, accountId) {
    return http.post(`/api/identify-codes/${channelId}/rotate-for-account`, { account_id: accountId })
  }
}

export default channelIdentifyCodeApi
