export default [{
  path: '/objection/list',
  name: 'ObjectionList',
  component: () => import('@/views/objection/List.vue'),
  meta: { menuKey: 'objectionList', menuSection: 'aiAgent', menuParents: ["agentSalesIntel"], menuOrder: 1, menuIcon: 'ChatLineRound', title: '异议处理', group: 'sales', icon: 'ChatLineRound', requiresAuth: true }
}];
