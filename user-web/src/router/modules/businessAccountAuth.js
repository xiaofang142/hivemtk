export default [
  {
    path: '/businessAccountAuth/list',
    name: 'BusinessAccountAuthList',
    component: () => import('@/views/businessAccountAuth/List.vue'),
    meta: { title: '业务账号授权', group: 'customer', icon: 'Lock', requiresAuth: true }
  }
]
