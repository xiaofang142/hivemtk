import { http } from '@/utils/request'

export const importSearchConsole = (payload) =>
  http.post('/api/geo/search-console/import', payload)

export const getDualEngineReport = (days = 30) =>
  http.get('/api/geo/reports/dual-engine', { days })
