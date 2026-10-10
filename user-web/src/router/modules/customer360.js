export default [
  {
    path: '/customer360/list',
    name: 'Customer360List',
    component: () => import('@/views/customer360/List.vue'),
    meta: { menuKey: 'customer360', menuSection: 'customer', menuOrder: 4, menuIcon: 'UserFilled', title: '客户 360', group: 'customer', icon: 'UserFilled', requiresAuth: true }
  }
]
