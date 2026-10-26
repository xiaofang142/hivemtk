import { http } from '@/utils/request'

export const messageHubApi = {
  pushMessage(data) {
    return http.post('/api/message-hub/push', data)
  },

  pushBatch(data) {
    return http.post('/api/message-hub/push-batch', data)
  },

  pushFromChannel(data) {
    return http.post('/api/message-hub/push-from-channel', data)
  },

  getMessages(params) {
    return http.get('/api/message-hub/list', params)
  },

  getMessageById(id) {
    return http.get(`/api/message-hub/${id}`)
  },

  markRead(ids) {
    return http.post(`/api/message-hub/${ids[0]}/read`, { ids })
  },

  /**
   * 撤回一条已发出的消息（B2）。
   *
   * 幂等：这一行已经撤过时后端回 200 + already_recalled=true，不会第二次打平台 API。
   * 失败分类靠 HTTP 码区分（后端 reason 文案可直接展示）：
   *   409 = 这个渠道没有主动撤回通道 / 已过平台窗口（等也没用）
   *   400 = 入站消息不是撤回对象
   *   503 = 撤回端口未装配
   *   其余非 2xx = 出网失败，可重试
   */
  recallMessage(id, config = {}) {
    return http.post(`/api/message-hub/${id}/recall`, undefined, config)
  },

  getStats(params) {
    return http.get('/api/message-hub/stats', params)
  },

  getPlatforms() {
    return http.get('/api/message-hub/platforms')
  }
};
