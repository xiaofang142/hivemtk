import { http } from '@/utils/request';

export function getLeadMiningStatus() {
  return http.get('/api/lead-mining/status')
}

export function getLeadMiningJudgements(params = {}) {
  const q = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v !== '' && v !== null && v !== undefined) q.set(k, String(v))
  })
  const qs = q.toString()
  const url = qs ? `/api/lead-mining/judgements?${qs}` : '/api/lead-mining/judgements'
  return http.get(url)
}

export function getLeadMiningClueStats() {
  return http.get('/api/lead-mining/clue-stats')
}

export function getLeadMiningNotifyRoutes(params = {}) {
  const q = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v !== '' && v !== null && v !== undefined) q.set(k, String(v))
  })
  const qs = q.toString()
  const url = qs ? `/api/lead-mining/notify-routes?${qs}` : '/api/lead-mining/notify-routes'
  return http.get(url)
}

export function createLeadMiningNotifyRoute(data) {
  return http.post('/api/lead-mining/notify-routes', data)
}

export function deleteLeadMiningNotifyRoute(routeId) {
  return http.delete(`/api/lead-mining/notify-routes/${encodeURIComponent(routeId)}`)
}

export function getLeadMiningJudgementDetail(judgementId) {
  return http.get(`/api/lead-mining/judgements/${encodeURIComponent(judgementId)}`)
}

export function submitLeadMiningJudgementFeedback(judgementId, data) {
  return http.post(`/api/lead-mining/judgements/${encodeURIComponent(judgementId)}/feedback`, data)
}

export function getLeadMiningQualityStats(days = 7) {
  return http.get(`/api/lead-mining/quality-stats?days=${encodeURIComponent(days)}`)
}

export function submitLeadMiningClueFeedback(clueId, data) {
  return http.post(`/api/clues/${encodeURIComponent(clueId)}/feedback`, data)
}
