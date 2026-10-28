import { http } from '@/utils/http'

// ego-lite 腿（浏览器自动化的第二种执行腿：用户自己的 ego-browser）
//
// 与 browser_automation 的分界：那一套是 NM Host + 扩展（平台托管执行），
// 这一套是用户本机 ego-browser + ego-runner（个人浏览器执行）。两套接口不共用前缀，
// token 也不通用（eg_ 前缀 vs bh_ 前缀），所以单独一个 api 文件，不做"顺手复用"。

// ========== 流程（用户的草稿 → 发布 → 提交审核） ==========

export const listEgoFlows = (params) => http.get('/api/egolite/flows', params)

export const getEgoFlow = (id) => http.get(`/api/egolite/flows/${id}`)

// 保存：id 走 query（服务端 Save 语义：0=新建，非 0=更新草稿并重扫）
export const saveEgoFlow = (id, data) =>
  http.post(id ? `/api/egolite/flows?id=${id}` : '/api/egolite/flows', data)

export const rescanEgoFlow = (id) => http.post(`/api/egolite/flows/${id}/rescan`)

export const publishEgoFlow = (id) => http.post(`/api/egolite/flows/${id}/publish`)

export const submitEgoFlow = (id) => http.post(`/api/egolite/flows/${id}/submit`)

export const dryRunEgoFlow = (id, data) =>
  http.post(`/api/egolite/flows/${id}/dry-run`, data, { _silent: true })

export const deleteEgoFlow = (id) => http.delete(`/api/egolite/flows/${id}`)

// 流程定时触发器（specs/016 S10；只挂纯读流程）
export const getEgoFlowCron = (id) => http.get(`/api/egolite/flows/${id}/cron`)
export const upsertEgoFlowCron = (id, data) => http.post(`/api/egolite/flows/${id}/cron`, data)
export const deleteEgoFlowCron = (id) => http.delete(`/api/egolite/flows/${id}/cron`)

// ========== 执行 ==========

export const listEgoExecutions = (params) => http.get('/api/egolite/executions', params)

export const getEgoExecution = (id) => http.get(`/api/egolite/executions/${id}`)

export const listEgoEvents = (id, params) =>
  http.get(`/api/egolite/executions/${id}/events`, params)

// 下发执行：draft=试跑某流程；market=执行目录资产（asset_id 即资产编码）
export const startEgoExecution = (data) =>
  http.post('/api/egolite/executions', data, { _silent: true })

export const abortEgoExecution = (id) => http.post(`/api/egolite/executions/${id}/abort`)

// 待下发执行重发（runner 上线后；服务端 409=状态已推进）
export const redispatchEgoExecution = (id) =>
  http.post(`/api/egolite/executions/${id}/redispatch`, {}, { _silent: true })

export const confirmEgoExecution = (id, allow) =>
  http.post(`/api/egolite/executions/${id}/confirm`, { allow })

// ========== runner ==========

export const getEgoRunnerStatus = () => http.get('/api/egolite/runner/status')

// 每用户多条令牌：列表只回 hint（明文只在签发那一刻返回一次）
export const listEgoRunnerTokens = () => http.get('/api/egolite/runner/tokens')

export const issueEgoRunnerToken = (label) =>
  http.post('/api/egolite/runner/tokens', { label })

export const revokeEgoRunnerToken = (id) =>
  http.post(`/api/egolite/runner/tokens/${id}/revoke`)

// ========== 资产目录 ==========

// 用户端橱窗（只有 published 可见）
export const listEgoAssets = () => http.get('/api/egolite/assets')

// 平台端（= 本实例管理端）
export const listEgoAdminAssets = (params) => http.get('/api/egolite/admin/assets', params)

export const listEgoAdminFlows = (params) => http.get('/api/egolite/admin/flows', params)

export const publishEgoAssetFromFlow = (flowId) =>
  http.post('/api/egolite/admin/assets/publish', { flow_id: flowId })

export const publishEgoAssetPack = (data) => http.post('/api/egolite/admin/assets/pack', data)

export const offlineEgoAsset = (id) => http.post(`/api/egolite/admin/assets/${id}/offline`)

// ========== 通用浏览器会话（ops 轮次协议） ==========

export const listEgoSessions = (params) => http.get('/api/egolite/sessions', params)

export const getEgoSession = (id) => http.get(`/api/egolite/sessions/${id}`)

export const createEgoSession = (data) => http.post('/api/egolite/sessions', data, { _silent: true })

export const runEgoSessionRound = (id, data) =>
  http.post(`/api/egolite/sessions/${id}/round`, data, { _silent: true })

export const observeEgoSession = (id, round = 0) =>
  http.post(`/api/egolite/sessions/${id}/observe`, { round })

export const listEgoSessionRounds = (id, params) =>
  http.get(`/api/egolite/sessions/${id}/rounds`, params)

export const confirmEgoSession = (id, allow) =>
  http.post(`/api/egolite/sessions/${id}/confirm`, { allow })

export const abortEgoSession = (id, reason = '用户停止') =>
  http.post(`/api/egolite/sessions/${id}/abort`, { reason })

export const finishEgoSession = (id, keep = []) =>
  http.post(`/api/egolite/sessions/${id}/finish`, { keep })
