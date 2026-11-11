import { http } from '@/utils/request';

export const listAssets = (params) =>
  http.get('/api/v1/asset-market/list', params);

export const assetDetail = (id) =>
  http.get(`/api/v1/asset-market/detail/${id}`)

export const purchaseAsset = (data) =>
  http.post('/api/v1/asset-market/purchase', data)

export const syncAsset = (data) =>
  http.post('/api/v1/asset-market/sync', data)

// AM-02：商户积分余额自查（透传平台 MerchantCreditView）
export const getAssetCredits = () =>
  http.get('/api/v1/asset-market/credits')

export const reportUsage = (data) =>
  http.post('/api/v1/asset-market/report-usage', data)

export const listLocalAssets = (params) =>
  http.get('/api/v1/local-assets', params);

export const getLocalAsset = (id) =>
  http.get(`/api/v1/local-assets/${id}`)

export const createLocalAsset = (data) =>
  http.post('/api/v1/local-assets', data)

export const updateLocalAsset = (id, data) =>
  http.put(`/api/v1/local-assets/${id}`, data)

export const deleteLocalAsset = (id) =>
  http.delete(`/api/v1/local-assets/${id}`)

export const toggleLocalAsset = (id, active) =>
  http.put(`/api/v1/local-assets/${id}/toggle-active`, { active })

export const syncLog = (params) =>
  http.get('/api/v1/local-assets/sync-log', params)
