import { http } from '@/utils/request'

// GEO 一键自动驾驶（单关键词全链路）
export const geoAutopilotApi = {
  createRun(data) {
    return http.post('/api/geo/autopilot/runs', data)
  },

  getRun(id) {
    return http.get(`/api/geo/autopilot/runs/${id}`)
  },

  listRuns(params) {
    return http.get('/api/geo/autopilot/runs', params)
  }
}

export default geoAutopilotApi
