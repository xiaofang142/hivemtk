import { http } from '@/utils/request'

export const listPlatforms = () =>
  http.get('/api/geo/platform/platforms');

export const listPlatformAccounts = () =>
  http.get('/api/geo/platform/accounts')

export const savePlatformAccount = (data) =>
  http.post('/api/geo/platform/accounts', data)

export const deletePlatformAccount = (id) =>
  http.delete(`/api/geo/platform/accounts/${id}`)

export const publishToPlatform = (articleId, platform, opts = {}) =>
  http.post('/api/geo/platform/publish', { article_id: articleId, platform, ...opts })

export const listPlatformPublishRecords = (articleId) =>
  http.get('/api/geo/platform/records', { article_id: articleId || '' })
