export default [
  {
    path: '/customer360/list',
    name: 'Customer360List',
    component: () => import('@/views/customer360/List.vue'),
    meta: { title: '客户 360', group: 'customer', icon: 'UserFilled', requiresAuth: true }
  },
  {
    // D04 客户同意时间线 / 偏好
    path: '/customer360/consents',
    name: 'CustomerConsents',
    component: () => import('@/views/customer360/Consents.vue'),
    meta: { title: '客户同意与偏好', group: 'customer', icon: 'DocumentChecked', requiresAuth: true }
  }
]
