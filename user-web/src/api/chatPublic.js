import axios from 'axios'

// ============================================================================
// 公开 chat API 封装（访客端，无 JWT）
// ----------------------------------------------------------------------------
// 私域部署模式：
//   - 不强制要求 AppKey
//   - channelId 通过 X-Chat-Channel-Id Header 传递（缺失时后端使用 default）
//   - visitorId 通过 X-Chat-Visitor-Id Header 传递（必须，用于会话归属）
// ============================================================================

const publicPost = async (url, data, channelId, visitorId, visitorToken) => {
  return axios.post(url, data, {
    baseURL: window.location.origin,
    headers: {
      'Content-Type': 'application/json',
      'X-Chat-Channel-Id': channelId,
      'X-Chat-Visitor-Id': visitorId,
      // 会话级操作需携带 OpenSession 返回的 visitor_token（后端 IDOR 防护）
      ...(visitorToken ? { 'X-Chat-Visitor-Token': visitorToken } : {})
    }
  }).then(res => res.data)
}

const publicGet = async (url, params, channelId, visitorId, visitorToken) => {
  return axios.get(url, {
    baseURL: window.location.origin,
    params,
    headers: {
      'X-Chat-Channel-Id': channelId,
      'X-Chat-Visitor-Id': visitorId,
      ...(visitorToken ? { 'X-Chat-Visitor-Token': visitorToken } : {})
    }
  }).then(res => res.data)
}

// 打开会话
export const openSession = (data, channelId, visitorId) => {
  return publicPost('/api/chat/public/sessions', data, channelId, visitorId)
}

// 获取活跃会话
export const getActiveSession = (channelId, visitorId) => {
  return publicGet('/api/chat/public/sessions/active', {}, channelId, visitorId)
}

// 获取最近已结束会话
export const getRecentClosedSessions = (channelId, visitorId, limit = 10) => {
  return publicGet('/api/chat/public/sessions/recent-closed', { limit }, channelId, visitorId)
}

// 获取历史消息
export const getMessages = (sessionId, page, pageSize, channelId, visitorId, visitorToken) => {
  return publicGet(`/api/chat/public/sessions/${sessionId}/messages`, { page, page_size: pageSize }, channelId, visitorId, visitorToken)
}

// 拉取离线消息
export const getOfflineMessages = (sessionId, channelId, visitorId, visitorToken) => {
  return publicGet(`/api/chat/public/sessions/${sessionId}/offline-messages`, {}, channelId, visitorId, visitorToken)
}

// 发送消息
export const sendMessage = (sessionId, body, channelId, visitorId, visitorToken) => {
  return publicPost(`/api/chat/public/sessions/${sessionId}/messages`, body, channelId, visitorId, visitorToken)
}

// 转人工
export const requestHumanTransfer = (sessionId, reason, channelId, visitorId, visitorToken) => {
  return publicPost(`/api/chat/public/sessions/${sessionId}/transfer`, { reason }, channelId, visitorId, visitorToken)
}

// 关闭会话
export const closeSession = (sessionId, channelId, visitorId, visitorToken) => {
  return publicPost(`/api/chat/public/sessions/${sessionId}/close`, {}, channelId, visitorId, visitorToken)
}

// 评分
export const rateSession = (sessionId, rating, comment, channelId, visitorId, visitorToken) => {
  return publicPost(`/api/chat/public/sessions/${sessionId}/rate`, { rating, comment }, channelId, visitorId, visitorToken)
}

// 为历史会话列表里选中的那一条换回它自己的 visitor_token。
// visitor_token 只随 openSession 返回，而 openSession 只会命中"最近活跃的那一条"，
// 所以从 recent-closed 列表点开更早的会话必须先换 token，否则该会话的
// messages / offline-messages / close / rate 会全部 403。
export const exchangeSessionToken = (sessionId, channelId, visitorId) => {
  return publicPost(`/api/chat/public/sessions/${sessionId}/token`, {}, channelId, visitorId)
}

// 可用坐席数
export const getAvailableAgents = (channelId) => {
  return publicGet('/api/chat/public/agents/available', {}, channelId, '')
}

// 附件上传凭证（七牛）。
// 后端 GET /api/chat/public/upload-token 要求 session_id + visitor_id 两个 query，
// 并用 X-Chat-Visitor-Token 验签（与其它公开 chat 口同一套访客凭证），所以必须走
// publicGet，不能用后台 JWT 的 http 实例 —— 后者既没带 session/visitor，也没有签名头。
export const getUploadToken = (params, channelId, visitorId, visitorToken, sessionId) => {
  return publicGet(
    '/api/chat/public/upload-token',
    { ...(params || {}), session_id: sessionId, visitor_id: visitorId },
    channelId,
    visitorId,
    visitorToken
  ).then((res) => (res && res.data !== undefined ? res.data : res))
}

export default {
  openSession,
  getActiveSession,
  getRecentClosedSessions,
  getMessages,
  getOfflineMessages,
  sendMessage,
  requestHumanTransfer,
  closeSession,
  rateSession,
  exchangeSessionToken,
  getAvailableAgents,
  getUploadToken
}
