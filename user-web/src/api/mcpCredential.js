import { http } from '@/utils/request'

// MCP 凭证对管理（ClientID + APIKey）
// 签发支持：手动 client_id / 绑定账户 owner_user_id / 过期时间（expires_at 或 ttl_days）
export const mcpCredentialApi = {
  list: () => http.get('/api/mcp/credentials'),
  create: (data) => http.post('/api/mcp/credentials', data),
  update: (id, data) => http.put(`/api/mcp/credentials/${id}`, data),
  remove: (id) => http.delete(`/api/mcp/credentials/${id}`)
}

// MCP 凭证钥匙（1:N）：一个 Client ID 名下多把 API Key，各自 启用/禁用/停用/删除。
// reveal 是敏感 GET（服务端强制落管理审计）；签发响应里的明文只出现一次。
export const mcpKeyApi = {
  list: (credentialId) => http.get(`/api/mcp/credentials/${credentialId}/keys`),
  create: (credentialId, data) => http.post(`/api/mcp/credentials/${credentialId}/keys`, data),
  enable: (credentialId, keyId) => http.post(`/api/mcp/credentials/${credentialId}/keys/${keyId}/enable`),
  disable: (credentialId, keyId) => http.post(`/api/mcp/credentials/${credentialId}/keys/${keyId}/disable`),
  revoke: (credentialId, keyId) => http.post(`/api/mcp/credentials/${credentialId}/keys/${keyId}/revoke`),
  reveal: (credentialId, keyId) => http.get(`/api/mcp/credentials/${credentialId}/keys/${keyId}/reveal`),
  remove: (credentialId, keyId) => http.delete(`/api/mcp/credentials/${credentialId}/keys/${keyId}`)
}

// MCP 调用审计（凭证级 scope 拒绝与成功调用都留痕；event=call/list/auth）
export const mcpAuditApi = {
  list: (params) => http.get('/api/mcp/audits', { params })
}

// MCP/Skill 管理面操作审计（凭证/钥匙/Skill 的增删改、状态流转、明文查看）
export const mcpAdminAuditApi = {
  list: (params) => http.get('/api/mcp/admin-audits', { params })
}

// Skill 注册表管理（说明 + 绑定凭证 + 可用工具集 + 红线提示）
export const mcpSkillApi = {
  list: () => http.get('/api/mcp/skills'),
  get: (id) => http.get(`/api/mcp/skills/${id}`),
  create: (data) => http.post('/api/mcp/skills', data),
  update: (id, data) => http.put(`/api/mcp/skills/${id}`, data),
  remove: (id) => http.delete(`/api/mcp/skills/${id}`)
}