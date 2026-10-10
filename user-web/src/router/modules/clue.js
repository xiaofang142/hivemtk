export default [
  {
    path: '/clue/list',
    name: 'ClueList',
    component: () => import('@/views/clue/List.vue'),
    meta: { menuKey: 'clueList', menuSection: 'customer', menuOrder: 1, menuIcon: 'Document', title: '线索列表', group: 'customer', icon: 'Document' }
  },
  {
    path: '/clue/statistics',
    name: 'ClueStatistics',
    component: () => import('@/views/clue/Statistics.vue'),
    meta: { menuKey: 'clueStatistics', menuSection: 'customer', menuOrder: 2, menuIcon: 'DataAnalysis', title: '线索统计', group: 'customer', icon: 'DataAnalysis' }
  }
]