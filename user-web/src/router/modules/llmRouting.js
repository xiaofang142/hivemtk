export default [
  {
    path: '/llmRouting/list',
    name: 'LlmRoutingList',
    component: () => import('@/views/llmRouting/List.vue'),
    meta: { menuKey: 'llmRouting', menuSection: 'aiAgent', menuParents: ["agentPassive"], menuOrder: 4, menuIcon: 'Cpu', title: 'LLM路由', group: 'aiAgent', icon: 'Cpu', requiresAuth: true }
  },
  {
    path: '/llmRouting/cost',
    name: 'LlmRoutingCostDashboard',
    component: () => import('@/views/llmRouting/CostDashboard.vue'),
    meta: { menuHidden: true, title: '成本看板', group: 'aiAgent', icon: 'DataLine', requiresAuth: true }
  }
]
