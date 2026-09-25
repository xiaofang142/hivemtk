export default [
  {
    path: '/badCase/list',
    name: 'BadCaseList',
    component: () => import('@/views/badCase/List.vue'),
    meta: { title: 'Bad Case 队列', group: 'workspace', icon: 'Warning' }
  }
]
