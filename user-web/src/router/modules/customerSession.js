export default [
  {
    path: '/customerSession/list',
    name: 'CustomerSessionList',
    component: () => import('@/views/customerSession/List.vue'),
    meta: { menuKey: 'customerSession', menuSection: 'aiAgent', menuParents: ["agentPassive"], menuOrder: 1, menuIcon: 'Service', title: '客服会话', group: 'customer', icon: 'Service', requiresAuth: true }
  }
]
