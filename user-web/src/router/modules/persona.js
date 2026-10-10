export default [{
  path: '/persona/list',
  name: 'PersonaList',
  component: () => import('@/views/persona/List.vue'),
  meta: { menuKey: 'personaList', menuSection: 'aiAgent', menuParents: ["agentSalesIntel"], menuOrder: 2, menuIcon: 'UserFilled', title: '销冠画像', group: 'sales', icon: 'UserFilled', requiresAuth: true }
}];
