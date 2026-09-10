import { http } from '@/utils/request'

// 统一收件箱：会话工作台 API（对应后端 controller/inbox.go InboxController）
export const inboxApi = {
  // 会话列表
  listConversations(params) {
    return http.get('/api/inbox/conversations', params)
  },

  // 会话详情
  getConversation(id) {
    return http.get(`/api/inbox/conversations/${id}`)
  },

  // 会话消息线程（hub + session 合并）
  getMessages(id, params) {
    return http.get(`/api/inbox/conversations/${id}/messages`, params)
  },

  // 标记已读
  markRead(id) {
    return http.post(`/api/inbox/conversations/${id}/read`)
  },

  // 置顶 / 星标 / 静音（pinned/starred/muted: bool）
  setPin(id, pinned) {
    return http.post(`/api/inbox/conversations/${id}/pin`, { pinned })
  },

  setStar(id, starred) {
    return http.post(`/api/inbox/conversations/${id}/star`, { starred })
  },

  setMute(id, muted) {
    return http.post(`/api/inbox/conversations/${id}/mute`, { muted })
  },

  // 标签
  addTag(id, tag) {
    return http.post(`/api/inbox/conversations/${id}/tags`, { tag })
  },

  removeTag(id, tag) {
    return http.delete(`/api/inbox/conversations/${id}/tags/${encodeURIComponent(tag)}`)
  },

  // 删除会话内单条消息（source: hub | session）
  deleteMessage(id, mid, source) {
    return http.delete(`/api/inbox/conversations/${id}/messages/${mid}?source=${source}`)
  },

  // 手动分配 action: assign/reassign/release/close/reopen；to_type: human/sop/ai
  assign(data) {
    return http.post('/api/inbox/assign', data)
  },

  // 自动分配 mode: least_load(默认) | round_robin
  autoAssign(data) {
    return http.post('/api/inbox/assign/auto', data)
  },

  // 分配历史
  listAssignments(params) {
    return http.get('/api/inbox/assignments', params)
  },

  // 统计
  getStats() {
    return http.get('/api/inbox/stats')
  },

  // 对账治理 mode: unread | overdue | backfill
  reconcile(mode) {
    return http.post(`/api/inbox/reconcile?mode=${mode}`)
  },

  // 坐席负载
  staffLoad(staff) {
    return http.get(`/api/inbox/staff/${encodeURIComponent(staff)}/load`)
  }
}

export default inboxApi
