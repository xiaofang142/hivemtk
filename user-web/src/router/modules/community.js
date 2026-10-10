export default [
  {
    path: '/community/list',
    name: 'CommunityList',
    component: () => import('@/views/community/List.vue'),
    meta: { menuKey: 'community', menuSection: 'reach', menuOrder: 12, menuIcon: 'UserFilled', title: '社群管理', group: 'community', icon: 'UserFilled' }
  }
]
