export default [
  {
    path: '/customerSession/list',
    name: 'CustomerSessionList',
    component: () => import('@/views/customerSession/List.vue'),
    meta: { title: '客服会话', group: 'customer', icon: 'Service', requiresAuth: true }
  }
]
