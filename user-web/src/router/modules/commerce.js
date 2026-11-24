export default [{
  path: '/commerce/operations',
  name: 'CommerceOperations',
  component: () => import('@/views/commerce/OperationsPanel.vue'),
  meta: { title: '电商操作工作台', group: 'workspace', icon: 'Document', requiresAuth: true }
}]
