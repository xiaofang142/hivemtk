import { http } from '@/utils/request';

export function getRFMList(params) {
  return http.get('/api/user-segment/rfm/list', params)
}
export function getRFMRule() {
  return http.get('/api/user-segment/rfm/rule')
}
export function saveRFMRule(data) {
  return http.post('/api/user-segment/rfm/rule', data)
}
export function updateRFMRule(id, data) {
  return http.put(`/api/user-segment/rfm/rule/${id}`, data)
}
export function getUserRFM(userId) {
  return http.get(`/api/user-segment/rfm/user?user_id=${userId}`)
}
export function getRFMStats() {
  return http.get('/api/user-segment/rfm/stats')
}
export function calculateRFM(data) {
  return http.post('/api/user-segment/rfm/calculate', data)
}
export function getLayerDescription() {
  return http.get('/api/user-segment/layers')
}

export function getUserSegments(params) {
  return http.get('/api/user-segments', params)
}
export function getSegmentStats() {
  return getRFMStats()
}

// toRFMRulePayload 把"分群弹窗"的单档阈值表单翻译成后端 RFM 规则契约
// （SaveRFMRuleRequest 只认 snake_case 五档 r_days_1..5/f_count_1..5/m_amount_1..5）。
// #1047：直接透传 { rDays, fCount, mAmount, criteria, remark, status } 时后端
// 一个字段都绑不上——200「保存成功」但阈值全部落默认值，用户设置静默丢失。
// 单档语义映射为"1 档边界"（rDays=近 rDays 天活跃、fCount=至少 fCount 次、
// mAmount=至少 mAmount 分）；criteria 并入 name 备注语义由调用方取舍，
// status→is_active（active 之外一律停用）。
function toRFMRulePayload(data = {}) {
  const payload = {
    name: data.name ?? data.remark ?? '',
    r_days_1: Number(data.rDays) > 0 ? Number(data.rDays) : undefined,
    f_count_1: Number(data.fCount) > 0 ? Number(data.fCount) : undefined,
    m_amount_1: Number(data.mAmount) > 0 ? Number(data.mAmount) : undefined,
    is_active: data.status ? data.status === 'active' : undefined
  }
  // 五档其余档位与后端默认对齐（后端零值会回落各自默认阈值）
  if (data.rDays != null) {
    payload.r_days_2 = 14; payload.r_days_3 = 30; payload.r_days_4 = 60; payload.r_days_5 = 90
  }
  if (data.fCount != null) {
    payload.f_count_2 = 3; payload.f_count_3 = 5; payload.f_count_4 = 10; payload.f_count_5 = 20
  }
  if (data.mAmount != null) {
    payload.m_amount_2 = 50000; payload.m_amount_3 = 100000; payload.m_amount_4 = 500000; payload.m_amount_5 = 1000000
  }
  Object.keys(payload).forEach((k) => payload[k] === undefined && delete payload[k])
  return payload
}

export function createUserSegment(data) {
  return saveRFMRule(toRFMRulePayload(data))
}
export function updateUserSegment(id, data) {
  return updateRFMRule(id, toRFMRulePayload(data))
}
export function deleteUserSegment(id) {
  return http.delete(`/api/user-segment/rfm/rule/${id}`)
}
export function getSegmentUsers(id, params) {
  return http.get(`/api/user-segment/rfm/list?segment_id=${id}`, params)
}
