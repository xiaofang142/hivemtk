export default [
  {
    path: '/tagSegmentation/list',
    name: 'TagSegmentationList',
    component: () => import('@/views/tagSegmentation/List.vue'),
    meta: { menuKey: 'tagSegmentation', menuSection: 'customer', menuOrder: 6, menuRoles: ["admin","manager","viewer"], menuIcon: 'PriceTag', title: '标签分层', group: 'customer', icon: 'PriceTag', requiresAuth: true }
  }
]
