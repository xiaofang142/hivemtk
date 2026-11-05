import { http } from '@/utils/request'

// 后端参数名是 search（不是 keyword），并且支持 page/limit 分页与 total 返回
export const listEntities = (type, keyword, page = 1, limit = 20) =>
  http.get('/api/geo/entity/list', {
    type: type || '',
    search: keyword || '',
    page: page || 1,
    limit: limit || 20
  });

export const getEntityRelations = (entityId) =>
  http.get(`/api/geo/entity/${entityId}/graph`)

export const extractEntities = (docId) =>
  http.post('/api/geo/entities/extract', { doc_id: docId })

export const listKBDocuments = (keyword, sourceLevel) =>
  http.get('/api/geo/kb/documents', { keyword: keyword || '', source_level: sourceLevel || '' });

export const saveKBDocument = (data) =>
  http.post('/api/geo/kb/documents', data)

export const deleteKBDocument = (id) =>
  http.delete(`/api/geo/kb/documents/${id}`)

export const getKBDocument = (id) =>
  http.get(`/api/geo/kb/documents/${id}`)

export const searchKB = (q, limit = 10) =>
  http.get('/api/geo/kb/search', { q, limit })

export const askKB = (question) =>
  http.post('/api/geo/kb/ask', { question })

export const listWorkflows = () =>
  http.get('/api/geo/workflow/workflows');

export const getWorkflow = (id) =>
  http.get(`/api/geo/workflow/workflows/${id}`)

export const saveWorkflow = (data) =>
  http.post('/api/geo/workflow/workflows', data)

export const updateWorkflow = (id, data) =>
  http.put(`/api/geo/workflow/workflows/${id}`, data)

export const deleteWorkflow = (id) =>
  http.delete(`/api/geo/workflow/workflows/${id}`)

export const runWorkflow = (id, params) =>
  http.post(`/api/geo/workflow/workflows/${id}/run`, params || {})

export const listWorkflowExecutions = (workflowId) =>
  http.get(`/api/geo/workflow/workflows/${workflowId}/executions`)

export const listWorkflowTemplates = () =>
  http.get('/api/geo/workflow/templates');

export const saveWorkflowTemplate = (data) =>
  http.post('/api/geo/workflow/templates', data)
