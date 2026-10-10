export default [
  {
    path: '/inbox/list',
    name: 'InboxList',
    component: () => import('@/views/inbox/List.vue'),
    meta: { menuKey: 'inbox', menuSection: 'workspace', menuOrder: 3, menuIcon: 'Box', title: '统一收件箱', group: 'workspace', icon: 'Box' }
  }
]
