// 团队（租户）管理路由（多租户 P0/P1/P2）：
// Teams 一页自适应两种角色——admin=全量管理，team_admin=仅本团队（合并原 MyTeam）；
// Billing 计费管理；RBACMatrix 权限矩阵。旧 role/permission 模块已并入退役。
export default [
  {
    path: '/system/teams',
    name: 'SystemTeams',
    component: () => import('@/views/system/Teams.vue'),
    meta: { menuKey: 'systemTeams', menuSection: 'system', menuParents: ["teamGroup"], menuOrder: 7, menuIcon: 'OfficeBuilding', title: '团队管理', group: 'system', icon: 'OfficeBuilding', requiresAuth: true, menuRoles: ["admin", "team_admin"] }
  },
  {
    path: '/system/billing',
    name: 'SystemBilling',
    component: () => import('@/views/system/Billing.vue'),
    meta: { menuKey: 'systemBilling', menuSection: 'system', menuParents: ["teamGroup"], menuOrder: 10, menuIcon: 'Money', title: '计费管理', group: 'system', icon: 'Money', requiresAuth: true, menuRoles: ["admin"] }
  },
  {
    path: '/system/rbac-matrix',
    name: 'SystemRBACMatrix',
    component: () => import('@/views/system/RBACMatrix.vue'),
    meta: { menuKey: 'systemRbacMatrix', menuSection: 'system', menuParents: ["teamGroup"], menuOrder: 9, menuIcon: 'Grid', title: '权限矩阵', group: 'system', icon: 'Grid', requiresAuth: true, menuRoles: ["admin"] }
  }
]
