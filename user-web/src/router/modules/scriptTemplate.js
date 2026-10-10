export default [
  {
    path: '/scriptTemplate/list',
    name: 'ScriptTemplateList',
    component: () => import('@/views/scriptTemplate/List.vue'),
    meta: { menuKey: 'scriptTemplate', menuSection: 'aiAgent', menuParents: ["agentSupport"], menuOrder: 6, menuIcon: 'ChatLineSquare', title: '话术库', group: 'aiAgent', icon: 'ChatLineSquare', requiresAuth: true }
  },
  {
    path: '/scriptTemplate/ab-stats',
    name: 'ScriptTemplateAbStats',
    component: () => import('@/views/scriptTemplate/AbStats.vue'),
    meta: { menuKey: 'scriptTemplateAbStats', menuSection: 'aiAgent', menuParents: ["agentSupport"], menuOrder: 7, menuIcon: 'DataLine', title: '话术AB测试', group: 'aiAgent', icon: 'DataLine', requiresAuth: true }
  }
]
