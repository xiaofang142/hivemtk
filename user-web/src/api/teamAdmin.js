import { http } from '@/utils/request'

// 团队（租户）管理 API（多租户 P0/P1）
// 超管面：/api/manage/teams*（admin 全量 CRUD + 成员调拨）
// 团队管理员面：/api/team/*（本团队，teamID 取自 JWT）
export const teamAdminApi = {
  list: (params) => http.get('/api/manage/teams', { params }),
  create: (data) => http.post('/api/manage/teams', data),
  update: (id, data) => http.put(`/api/manage/teams/${id}`, data),
  remove: (id) => http.delete(`/api/manage/teams/${id}`),
  members: (id, params) => http.get(`/api/manage/teams/${id}/users`, { params }),
  setMemberRole: (teamId, userId, role) => http.put(`/api/manage/teams/${teamId}/users/${userId}/role`, { role }),
  moveMember: (teamId, userId) => http.post(`/api/manage/teams/${teamId}/users/${userId}/move`)
}

// RBAC 矩阵（P1-2）：权限点/角色只读出参
export const rbacApi = {
  matrix: () => http.get('/api/manage/rbac-matrix'),
  myPermissions: () => http.get('/api/auth/my-permissions')
}

export const myTeamApi = {
  detail: () => http.get('/api/team/detail'),
  members: (params) => http.get('/api/team/users', { params }),
  setMemberRole: (userId, role) => http.put(`/api/team/users/${userId}/role`, { role })
}
