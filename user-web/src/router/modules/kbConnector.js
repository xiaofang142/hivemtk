export default [
  {
    path: '/kb/connectors',
    name: 'KbConnectors',
    component: () => import('@/views/kb/Connectors.vue'),
    meta: { menuKey: 'knowledgeConnectors', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 10, menuRoles: ["admin"], menuIcon: 'Link', title: '知识库连接器', group: 'knowledge', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/cap/matrix',
    name: 'CapabilityMatrix',
    component: () => import('@/views/cap/Matrix.vue'),
    meta: { menuKey: 'capabilityMatrix', menuSection: 'system', menuOrder: 1, menuRoles: ["admin"], menuIcon: 'Grid', title: '能力矩阵', group: 'system', icon: 'Grid', requiresAuth: true }
  }
]
