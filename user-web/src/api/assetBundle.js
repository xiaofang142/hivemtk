import { http } from '@/utils/request';

export const createBundle = (data) =>
  http.post('/api/asset-bundle', data);

export const updateBundle = (id, data) =>
  http.put(`/api/asset-bundle/${id}`, data);

export const getBundle = (id) =>
  http.get(`/api/asset-bundle/${id}`);

export const getBundleByAssetID = (aid) =>
  http.get(`/api/asset-bundle/by-aid/${aid}`);

export const listBundles = (data) =>
  http.post('/api/asset-bundle/list', data);

export const publishBundle = (id) =>
  http.post(`/api/asset-bundle/${id}/publish`);

export const submitToPlatform = (id) =>
  http.post(`/api/asset-bundle/${id}/submit-platform`);

// 拉取平台审核状态并落库（审核结果回程：平台不回调，本地拉取闭环）
export const refreshPlatformStatus = (id) =>
  http.post(`/api/asset-bundle/${id}/platform-status/refresh`);

// 版本轨迹（审计回溯）
export const getBundleVersions = (id, limit = 50) =>
  http.get(`/api/asset-bundle/${id}/versions?limit=${limit}`);

export const archiveBundle = (id) =>
  http.post(`/api/asset-bundle/${id}/archive`);

export const deleteBundle = (id) =>
  http.delete(`/api/asset-bundle/${id}`);

export const weaveBundle = (data) =>
  http.post('/api/asset-bundle/weave', data);

export const merchantSave = (data) =>
  http.post('/api/asset-bundle/merchant-save', data);

export const merchantParse = (aid) =>
  http.post(`/api/asset-bundle/merchant-parse/${aid}`);

export const enableBundle = (id) =>
  http.post(`/api/asset-bundle/${id}/enable`);

export const disableBundle = (id) =>
  http.post(`/api/asset-bundle/${id}/disable`);

export const listEnabledBundles = () =>
  http.post('/api/asset-bundle/enabled/list');

export const uploadCover = (file) => {
  const fd = new FormData()
  fd.append('file', file)
  return http.post('/api/upload', fd, {
    headers: { 'Content-Type': 'multipart/form-data' }
  })
};
