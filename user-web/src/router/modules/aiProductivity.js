export default [
  {
    path: '/aiProductivity/list',
    name: 'AiProductivityList',
    component: () => import('@/views/aiProductivity/List.vue'),
    meta: { menuKey: 'aiProductivity', menuSection: 'analytics', menuOrder: 3, menuRoles: ["admin","manager","viewer"], menuIcon: 'DataAnalysis', title: 'AI产能分析', group: 'analytics', icon: 'DataAnalysis', requiresAuth: true }
  }
]
