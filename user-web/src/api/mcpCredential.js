import { http } from '@/utils/request'

// MCP 凭证对管理（ClientID + APIKey）
export const mcpCredentialApi = {
  list: () => http.get('/api/mcp/credentials'),
  create: (data) => http.post('/api/mcp/credentials', data),
  update: (id, data) => http.put(`/api/mcp/credentials/${id}`, data),
  remove: (id) => http.delete(`/api/mcp/credentials/${id}`)
}
