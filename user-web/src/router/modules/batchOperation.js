export default [
  {
    path: '/batchOperation/list',
    name: 'BatchOperationList',
    component: () => import('@/views/batchOperation/List.vue'),
    meta: { menuKey: 'batchOperation', menuSection: 'aiAgent', menuParents: ["agentActive"], menuOrder: 3, menuIcon: 'Operation', title: '批量操作', group: 'reach', icon: 'Operation', requiresAuth: true }
  }
]
