export default [
  {
    path: '/integration/list',
    name: 'IntegrationList',
    component: () => import('@/views/integration/List.vue'),
    meta: { menuKey: 'integration', menuSection: 'system', menuParents: ["permissionGroup"], menuOrder: 2, menuIcon: 'Connection', title: '第三方对接', group: 'system', icon: 'Connection', requiresAuth: true, requiresAdmin: true }
  }
]
