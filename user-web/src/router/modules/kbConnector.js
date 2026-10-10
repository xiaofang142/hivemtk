export default [
  {
    path: '/kb/connectors',
    name: 'KbConnectors',
    component: () => import('@/views/kb/Connectors.vue'),
    meta: { title: '知识库连接器', group: 'knowledge', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/cap/matrix',
    name: 'CapabilityMatrix',
    component: () => import('@/views/cap/Matrix.vue'),
    meta: { title: '能力矩阵', group: 'system', icon: 'Grid', requiresAuth: true }
  }
]
