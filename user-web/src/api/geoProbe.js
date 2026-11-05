import { http } from '@/utils/request'

export const listProbeEngines = () =>
  http.get('/api/geo/probe/engines');

// 引擎名 + 数据来源（simulated / real）。UI 必须按此标注，不能把模拟写成真实。
export const listProbeEngineMeta = () =>
  http.get('/api/geo/probe/engines/meta');

export const importProbeRuns = (rows) =>
  http.post('/api/geo/probe/import', { rows })

export const testEngineProbe = (engine, query) =>
  http.post('/api/geo/probe/test', { engine, query })

export const listProbeRuns = (engine, limit = 100) =>
  http.get('/api/geo/probe/runs', { engine, limit })

export const runNegativeMonitor = () =>
  http.post('/api/geo/probe/run-negative')

export const runSourceCatalogSync = () =>
  http.post('/api/geo/probe/run-source-sync')

export const probeAllEngines = (engines, query) =>
  http.post('/api/geo/probe/all', { engines, query })

export const getSOV = () =>
  http.get('/api/geo/sov');

export const getCrawlerStats = () =>
  http.get('/api/geo/crawler-stats')

export const runCrawler = () =>
  http.post('/api/geo/crawler/run')

export const detectInaccurateClaims = (brandName) =>
  http.post('/api/geo/inaccurate-claims', { brand_name: brandName })

export const lookupSourceLevel = (url) =>
  http.get('/api/geo/source-catalog/levels', { url });

export const listCompetitors = () =>
  http.get('/api/geo/competitors');

export const getCompetitor = (id) =>
  http.get(`/api/geo/competitors/${id}`)

export const createCompetitor = (data) =>
  http.post('/api/geo/competitors', data)

export const updateCompetitor = (id, data) =>
  http.put(`/api/geo/competitors/${id}`, data)

export const deleteCompetitor = (id) =>
  http.delete(`/api/geo/competitors/${id}`)

export const getEngineCompare = (days = 30, intent = '') =>
  http.get('/api/geo/visibility/engine-compare', { days, intent: intent || '' })

export const getSOVTrend = (days = 30, intent = '') =>
  http.get('/api/geo/sov/trend', { days, intent: intent || '' })
