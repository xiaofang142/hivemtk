export default [
  {
    path: '/badCase/list',
    name: 'BadCaseList',
    component: () => import('@/views/badCase/List.vue'),
    meta: { menuKey: 'badCase', menuSection: 'workspace', menuOrder: 5, menuIcon: 'Warning', title: 'Bad Case 队列', group: 'workspace', icon: 'Warning' }
  }
]
