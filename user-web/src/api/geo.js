import { http } from '@/utils/request'

export const geoApi = {
  mineKeywords(data) {
    return http.post('/api/geo/keywords/mine', data)
  },

  semanticExpand(data) {
    return http.post('/api/geo/keywords/expand', data)
  },

  topicCluster(data) {
    return http.post('/api/geo/keywords/cluster', data)
  },

  getKeywordList(params) {
    return http.get('/api/geo/keywords/list', params)
  },

  // ⬇️ GEO v2：关键词蒸馏（下拉词 / 长尾组合 / 漏斗）
  crawlSuggest(data) {
    return http.post('/api/geo/keyword-mining/crawl-suggest', data)
  },

  combineLongtail(data) {
    return http.post('/api/geo/keyword-mining/longtail', data)
  },

  getKeywordFunnel() {
    return http.get('/api/geo/keyword-mining/funnel')
  },

  // ⬇️ GEO v2：蜘蛛推送
  pushUrls(data) {
    return http.post('/api/geo/push/urls', data)
  },

  getPushQuota() {
    return http.get('/api/geo/push/quota')
  },

  getSitemapPreview() {
    return http.get('/api/geo/push/sitemap')
  },

  // ⬇️ GEO v2：收录追踪
  verifyIndexFull(articleId) {
    return http.post(`/api/geo/index-tracking/verify/${articleId}`)
  },

  getIndexFunnel() {
    return http.get('/api/geo/index-tracking/funnel')
  },

  verifyAllIndex() {
    return http.post('/api/geo/index-tracking/verify-all')
  },

  // ⬇️ GEO v2：引用漂移 / 域名 / 矩阵
  getCitationDrift(params = {}) {
    return http.get('/api/geo/citations/drift', params)
  },

  getCitationDomains(days = 30) {
    return http.get('/api/geo/citations/domains', { days })
  },

  getCitationMatrix(days = 30, topQueries = 20, topDomains = 10) {
    return http.get('/api/geo/citations/matrix', { days, top_queries: topQueries, top_domains: topDomains })
  },

  downloadCitationCSV(kind, params = {}) {
    const path = kind === 'matrix' ? '/api/geo/citations/matrix' : '/api/geo/citations/domains'
    return http.get(path, { ...params, format: 'csv' }, { responseType: 'blob' })
  },

  // ⬇️ GEO v2：静态站部署
  exportSite() {
    return http.post('/api/geo/site/export')
  },

  deploySite() {
    return http.post('/api/geo/site/deploy', undefined, { _silent: true })
  },

  siteFullPipeline() {
    return http.post('/api/geo/site/full-pipeline', undefined, { _silent: true })
  },

  getLlmsTxtPreview(domain) {
    return http.get('/api/geo/site/llms-txt-preview', domain ? { domain } : {})
  },

  getRobotsTxtPreview() {
    return http.get('/api/geo/site/robots-preview')
  },

  // ⬇️ GEO v2：站点配置 CRUD
  listSites(params) {
    return http.get('/api/geo/sites', params)
  },

  createSite(data) {
    return http.post('/api/geo/sites', data)
  },

  updateSite(id, data) {
    return http.put(`/api/geo/sites/${id}`, data)
  },

  deleteSite(id) {
    return http.delete(`/api/geo/sites/${id}`)
  },

  // ⬇️ GEO v2：推送平台配置 CRUD
  listPushers(params) {
    return http.get('/api/geo/pushers', params)
  },

  createPusher(data) {
    return http.post('/api/geo/pushers', data)
  },

  updatePusher(id, data) {
    return http.put(`/api/geo/pushers/${id}`, data)
  },

  deletePusher(id) {
    return http.delete(`/api/geo/pushers/${id}`)
  },

  // ⬇️ GEO v2：Schema 模板 CRUD
  listSchemaTemplates(params) {
    return http.get('/api/geo/schema-templates', params)
  },

  createSchemaTemplate(data) {
    return http.post('/api/geo/schema-templates', data)
  },

  updateSchemaTemplate(id, data) {
    return http.put(`/api/geo/schema-templates/${id}`, data)
  },

  deleteSchemaTemplate(id) {
    return http.delete(`/api/geo/schema-templates/${id}`)
  },

  deleteKeyword(id) {
    return http.delete(`/api/geo/keywords/${id}`)
  },

  generateContent(data) {
    return http.post('/api/geo/content/generate', data)
  },

  // B2 批量生成（同步有界批，上限 20；逐条容错，truncated_to 回传截断值）
  batchGenerateContent(data) {
    return http.post('/api/geo/content/batch-generate', data)
  },

  scoreContent(data) {
    return http.post('/api/geo/content/score', data)
  },

  optimizeContent(data) {
    return http.post('/api/geo/content/optimize', data)
  },

  enhanceEEAT(data) {
    return http.post('/api/geo/content/eeat', data)
  },

  generateSchema(data) {
    return http.post('/api/geo/content/schema', data)
  },

  checkUniqueness(data) {
    return http.post('/api/geo/content/uniqueness', data)
  },

  getArticleList(params) {
    return http.get('/api/geo/content/list', params)
  },

  getArticleByID(id) {
    return http.get(`/api/geo/content/${id}`)
  },

  verifyArticle(data) {
    return http.post('/api/geo/verification/verify', data)
  },

  monitorNegative(data) {
    return http.post('/api/geo/verification/negative', data)
  },

  getVerifyResults(articleId) {
    return http.get(`/api/geo/verification/results/${articleId}`)
  },

  getReport(params) {
    return http.get('/api/geo/reports/summary', params)
  },

  getROI(params) {
    return http.get('/api/geo/reports/roi', params)
  },

  getAPICosts(params) {
    return http.get('/api/geo/reports/api-costs', params)
  },

  getContentFormatAttribution(days = 30) {
    return http.get('/api/geo/reports/content-format', { days })
  },

  getConfig() {
    return http.get('/api/geo/config')
  },

  updateConfig(data) {
    return http.put('/api/geo/config', data)
  },

  optimizeConfig(data) {
    return http.post('/api/geo/config/optimize', data)
  }
};

export default geoApi
