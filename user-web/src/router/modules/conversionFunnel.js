export default [
  {
    path: '/conversionFunnel/list',
    name: 'ConversionFunnelList',
    component: () => import('@/views/conversionFunnel/List.vue'),
    meta: { menuKey: 'conversionFunnel', menuSection: 'analytics', menuOrder: 2, menuIcon: 'Filter', title: '转化漏斗', group: 'analytics', icon: 'Filter', requiresAuth: true }
  }
]
