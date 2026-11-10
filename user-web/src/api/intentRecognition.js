import { http } from '@/utils/request'

export const intentApi = {
  recognize(data) {
    return http.post('/api/intent/recognize', data)
  },

  batchRecognize(data) {
    return http.post('/api/intent/recognize/batch', data)
  },

  getStats(params) {
    return http.get('/api/intent/stats', params)
  },

  getRecent(params) {
    return http.get('/api/intent/recent', params)
  },

  getDict() {
    return http.get('/api/intent/dict')
  },

  recognizeFine(data) {
    return http.post('/api/intent/recognize/fine', data)
  },

  getLogs(params) {
    return http.get('/api/intent/logs', params)
  },

  getStatsFine(params) {
    return http.get('/api/intent/stats/fine', params)
  },

  getConfig() {
    return http.get('/api/intent/config')
  },

  updateConfig(data) {
    return http.put('/api/intent/config', data)
  },

  getKeywordOverride() {
    return http.get('/api/intent-records/keywords-override')
  },

  updateKeywordOverride(data) {
    return http.put('/api/intent-records/keywords-override', data)
  },

  // TB-09 意图纠错反馈：改判→原文作词面写覆盖（热生效）
  correctIntent(data) {
    return http.post('/api/intent/feedback/correct', data)
  },

  getIntentOverride() {
    return http.get('/api/intent/feedback/override')
  },
}
