export default [
  {
    path: '/customReport/list',
    name: 'CustomReportList',
    component: () => import('@/views/customReport/List.vue'),
    meta: { menuKey: 'customReport', menuSection: 'analytics', menuOrder: 4, menuIcon: 'Document', title: '自定义报表', group: 'dataAnalysis', icon: 'Document', requiresAuth: true }
  }
]
