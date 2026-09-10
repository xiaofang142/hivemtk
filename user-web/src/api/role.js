// role.js 角色管理 API
//
// 阶段 5：角色管理全栈
// 路由前缀：/api/system/roles
// 后端：router/role_routes.go（受 RequireAdminMiddleware 保护）
import { http } from '@/utils/request'

// 列出 3 档系统角色 + 成员数
export function listRoles() {
  return http.get('/api/system/roles')
}

// 单个角色详情（带成员数）
export function getRole(code) {
  return http.get(`/api/system/roles/${code}`)
}

// 角色下成员列表（分页）
//   page: 1-based
//   size: 每页条数
export function listRoleMembers(code, params) {
  return http.get(`/api/system/roles/${code}/members`, { params })
}

// 命名导出聚合对象
export const roleApi = {
  list: listRoles,
  get: getRole,
  listMembers: listRoleMembers
}

export default roleApi
