export default [
  {
    path: '/inbox/list',
    name: 'InboxList',
    component: () => import('@/views/inbox/List.vue'),
    meta: { title: '统一收件箱', group: 'workspace', icon: 'Box' }
  }
]
