import { http } from '@/utils/request'

// MCP 凭证对管理（ClientID + APIKey）
// 签发支持：手动 client_id / 绑定账户 owner_user_id / 过期时间（expires_at 或 ttl_days）
export const mcpCredentialApi = {
  list: () => http.get('/api/mcp/credentials'),
  create: (data) => http.post('/api/mcp/credentials', data),
  update: (id, data) => http.put(`/api/mcp/credentials/${id}`, data),
  remove: (id) => http.delete(`/api/mcp/credentials/${id}`)
}

// MCP 调用审计（凭证级 scope 拒绝与成功调用都留痕）
export const mcpAuditApi = {
  list: (params) => http.get('/api/mcp/audits', { params })
}

// Skill 注册表管理（说明 + 绑定凭证 + 可用工具集 + 红线提示）
export const mcpSkillApi = {
  list: () => http.get('/api/mcp/skills'),
  get: (id) => http.get(`/api/mcp/skills/${id}`),
  create: (data) => http.post('/api/mcp/skills', data),
  update: (id, data) => http.put(`/api/mcp/skills/${id}`, data),
  remove: (id) => http.delete(`/api/mcp/skills/${id}`)
}