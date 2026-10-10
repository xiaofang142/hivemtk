export default [
  {
    path: '/marketingFlow/list',
    name: 'MarketingFlowList',
    component: () => import('@/views/marketingFlow/List.vue'),
    meta: { menuKey: 'marketingFlow', menuSection: 'aiAgent', menuParents: ["agentActive"], menuOrder: 2, menuRoles: ["admin","manager"], menuIcon: 'SetUp', title: '营销自动化', group: 'reach', icon: 'SetUp', requiresAuth: true }
  }
]
