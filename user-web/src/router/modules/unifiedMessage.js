export default [
  {
    path: '/unifiedMessage/list',
    name: 'UnifiedMessageList',
    component: () => import('@/views/unifiedMessage/List.vue'),
    meta: { menuKey: 'unifiedMessage', menuSection: 'customer', menuOrder: 8, menuIcon: 'Message', title: '统一消息', group: 'customer', icon: 'Message' }
  }
]
