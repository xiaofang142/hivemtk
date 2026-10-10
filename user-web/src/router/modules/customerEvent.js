export default [
  {
    path: '/customerEvent/list',
    name: 'CustomerEventList',
    component: () => import('@/views/customerEvent/List.vue'),
    meta: { menuKey: 'customerEvent', menuSection: 'customer', menuOrder: 5, menuIcon: 'Bell', title: '客户事件', group: 'customer', icon: 'Bell', requiresAuth: true }
  }
]
