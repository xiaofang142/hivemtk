import { http } from '@/utils/request'

export const knowledgeAPI = {
  importUpload(formData) {
    return http.post('/api/knowledge/import/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
  },
  importText(data) {
    return http.post('/api/knowledge/import/text', data)
  },
  importURL(data) {
    return http.post('/api/knowledge/import/url', data)
  },

  listDocuments(params) {
    return http.get('/api/knowledge/documents', { params })
  },
  getDocument(id, params) {
    return http.get(`/api/knowledge/documents/${id}`, { params })
  },
  getDocumentProgress(id, params) {
    return http.get(`/api/knowledge/documents/${id}/progress`, { params })
  },
  getDocumentChunks(id) {
    return http.get(`/api/knowledge/documents/${id}/chunks`)
  },
  updateDocument(id, data) {
    return http.put(`/api/knowledge/documents/${id}`, data)
  },
  deleteDocument(id, params) {
    return http.delete(`/api/knowledge/documents/${id}`, { params })
  },
  reindexDocument(id, params) {
    return http.post(`/api/knowledge/documents/${id}/reindex`, null, { params })
  },

  rebuildProductIndex(productId) {
    return http.post(`/api/knowledge/products/${productId}/rebuild-index`)
  },
  getProductOverview(productId) {
    return http.get(`/api/knowledge/products/${productId}/overview`)
  },

  search(data) {
    return http.post('/api/knowledge/search', data)
  },

  listImportLogs(params) {
    return http.get('/api/knowledge/import-logs', { params })
  },

  listOpenAPISources(params) {
    return http.get('/api/knowledge/openapi/sources', { params })
  },
  createOpenAPISource(data) {
    return http.post('/api/knowledge/openapi/sources', data)
  },
  getOpenAPISource(id, params) {
    return http.get(`/api/knowledge/openapi/sources/${id}`, { params })
  },
  updateOpenAPISource(id, data) {
    return http.put(`/api/knowledge/openapi/sources/${id}`, data)
  },
  deleteOpenAPISource(id, params) {
    return http.delete(`/api/knowledge/openapi/sources/${id}`, { params })
  },
  syncOpenAPISource(id, params) {
    return http.post(`/api/knowledge/openapi/sources/${id}/sync`, null, { params })
  },
  testOpenAPISource(id, data) {
    return http.post(`/api/knowledge/openapi/sources/${id}/test`, data)
  },
  toggleOpenAPISource(id, data, params) {
    return http.post(`/api/knowledge/openapi/sources/${id}/toggle`, data, { params })
  },

  getOverviewStats(params) {
    return http.get('/api/knowledge/stats/overview', { params })
  },
  getDocumentStats(params) {
    return http.get('/api/knowledge/stats/documents', { params })
  },
  getSearchStats(params) {
    return http.get('/api/knowledge/stats/searches', { params })
  },
  getImportStats(params) {
    return http.get('/api/knowledge/stats/imports', { params })
  },
  getOpenAPIStats(params) {
    return http.get('/api/knowledge/stats/openapi', { params })
  }
};

export default knowledgeAPI
