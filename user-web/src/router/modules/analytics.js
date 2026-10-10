export default [{
  path: '/analytics/cohort-path',
  name: 'CohortPath',
  component: () => import('@/views/analytics/CohortPath.vue'),
  meta: { menuKey: 'cohortPath', menuSection: 'analytics', menuOrder: 8, menuRoles: ["admin","manager","viewer"], menuIcon: 'Guide', title: '留存与路径', group: 'analytics', icon: 'TrendCharts', requiresAuth: true }
}];
