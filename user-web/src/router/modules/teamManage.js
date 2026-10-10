// 团队（租户）管理路由：超管面 Teams + 团队管理员面 MyTeam（多租户 P0/P1）
export default [
  {
    path: '/system/teams',
    name: 'SystemTeams',
    component: () => import('@/views/system/Teams.vue'),
    meta: { menuKey: 'systemTeams', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 7, menuIcon: 'OfficeBuilding', title: '团队管理', group: 'system', icon: 'OfficeBuilding', requiresAuth: true, menuRoles: ["admin"] }
  },
  {
    path: '/system/rbac-matrix',
    name: 'SystemRBACMatrix',
    component: () => import('@/views/system/RBACMatrix.vue'),
    meta: { menuKey: 'systemRbacMatrix', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 9, menuIcon: 'Grid', title: '权限矩阵', group: 'system', icon: 'Grid', requiresAuth: true, menuRoles: ["admin"] }
  },
  {
    path: '/system/my-team',
    name: 'SystemMyTeam',
    component: () => import('@/views/system/MyTeam.vue'),
    meta: { menuKey: 'systemMyTeam', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 8, menuIcon: 'UserFilled', title: '我的团队', group: 'system', icon: 'UserFilled', requiresAuth: true, menuRoles: ["admin", "team_admin"] }
  }
]
