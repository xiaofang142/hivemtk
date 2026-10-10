export default [
  {
    path: '/abExperiment/list',
    name: 'AbExperimentList',
    component: () => import('@/views/abExperiment/List.vue'),
    meta: { menuKey: 'abExperiment', menuSection: 'analytics', menuOrder: 5, menuRoles: ["admin","manager","viewer"], menuIcon: 'DataLine', title: 'A/B 实验', group: 'dataAnalysis', icon: 'DataLine', requiresAuth: true }
  }
]
