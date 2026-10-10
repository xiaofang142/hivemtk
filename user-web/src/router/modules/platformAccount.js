export default [
  {
    path: '/platformAccount/list',
    name: 'PlatformAccountList',
    component: () => import('@/views/platformAccount/List.vue'),
    meta: { menuKey: 'platformAccount', menuSection: 'system', menuParents: ["permissionGroup"], menuOrder: 1, menuIcon: 'Platform', title: '平台账号', group: 'system', icon: 'Platform', requiresAuth: true, requiresAdmin: true }
  }
]
