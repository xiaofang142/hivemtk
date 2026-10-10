import { http } from '@/utils/request'

// 第三方知识库连接器（通用框架：type 注册制）
export const kbConnectorApi = {
  list: () => http.get('/api/kb-connectors'),
  create: (data) => http.post('/api/kb-connectors', data),
  update: (id, data) => http.put(`/api/kb-connectors/${id}`, data),
  remove: (id) => http.delete(`/api/kb-connectors/${id}`),
  sync: (id) => http.post(`/api/kb-connectors/${id}/sync`)
}

// 渠道能力矩阵
export const capabilitiesApi = {
  get: () => http.get('/api/capabilities')
}
