import { getUploadToken as publicGetUploadToken } from './chatPublic'

// 访客端附件上传凭证。
//
// 契约见 chatPublic.getUploadToken：后端需要 session_id/visitor_id query 与
// X-Chat-Visitor-Token 头。这里保留旧的导入名，内部转走访客封装，避免调用方
// 再踩「用后台 JWT 实例打公开接口 → 恒 400 缺少 session_id」的坑。
// 参数：{ file_type, ext, size }, channelId, visitorId, visitorToken, sessionId
export function getUploadToken(params, channelId, visitorId, visitorToken, sessionId) {
  return publicGetUploadToken(params, channelId, visitorId, visitorToken, sessionId)
}
