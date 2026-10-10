export default [
  {
    path: '/churnPrediction/list',
    name: 'ChurnPredictionList',
    component: () => import('@/views/churnPrediction/List.vue'),
    meta: { menuKey: 'churnPrediction', menuSection: 'analytics', menuOrder: 6, menuRoles: ["admin","manager","viewer"], menuIcon: 'Warning', title: '流失预警', group: 'dataAnalysis', icon: 'Warning', requiresAuth: true }
  }
]
