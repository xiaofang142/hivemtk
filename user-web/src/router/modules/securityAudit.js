export default [
  {
    path: '/securityAudit/list',
    name: 'SecurityAuditList',
    component: () => import('@/views/securityAudit/List.vue'),
    meta: { menuKey: 'securityAudit', menuSection: 'system', menuParents: ["permissionGroup"], menuOrder: 5, menuIcon: 'Shield', title: '安全审计', group: 'system', icon: 'Shield', requiresAuth: true, requiresAdmin: true }
  }
]
