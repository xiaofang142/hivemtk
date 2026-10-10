export default [
  {
    path: '/sopAgent/list',
    name: 'SopAgentList',
    component: () => import('@/views/sopAgent/List.vue'),
    meta: { menuKey: 'sopAgent', menuSection: 'aiAgent', menuParents: ["agentSupport"], menuOrder: 1, menuIcon: 'Connection', title: '销冠 SOP 智能体', group: 'aiAgent', icon: 'Connection', requiresAuth: true }
  }
]
