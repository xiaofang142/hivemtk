export default [
  {
    path: '/operationLog/list',
    name: 'OperationLogList',
    component: () => import('@/views/operationLog/List.vue'),
    meta: { menuKey: 'operationLog', menuSection: 'system', menuParents: ["permissionGroup"], menuOrder: 4, menuIcon: 'Tickets', title: '操作日志', group: 'system', icon: 'Tickets', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/operationLog/enhanced',
    name: 'OperationLogEnhanced',
    component: () => import('@/views/operationLog/Enhanced.vue'),
    meta: { menuHidden: true, title: '操作日志(增强)', group: 'system', icon: 'Document', requiresAuth: true, requiresAdmin: true }
  }
]
