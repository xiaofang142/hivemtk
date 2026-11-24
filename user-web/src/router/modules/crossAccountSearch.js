export default [
  {
    path: '/crossAccountSearch/list',
    name: 'CrossAccountSearchList',
    component: () => import('@/views/crossAccountSearch/List.vue'),
    meta: { title: '跨账号客户检索', group: 'customer', icon: 'Search', requiresAuth: true }
  }
]
