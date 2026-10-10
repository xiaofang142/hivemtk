export default [
  {
    path: '/userSegment/list',
    name: 'UserSegmentList',
    component: () => import('@/views/userSegment/List.vue'),
    meta: { menuKey: 'userSegment', menuSection: 'customer', menuOrder: 7, menuRoles: ["admin","manager","viewer"], menuIcon: 'PieChart', title: '用户分层 RFM', group: 'customer', icon: 'PieChart', requiresAuth: true }
  },
  {
    path: '/userSegment/rfm-matrix',
    name: 'RfmMatrix',
    component: () => import('@/views/userSegment/RfmMatrix.vue'),
    meta: { menuHidden: true, title: 'RFM 矩阵', group: 'analytics', icon: 'Grid', requiresAuth: true }
  },
  {
    path: '/userSegment/builder',
    name: 'UserSegmentBuilder',
    component: () => import('@/views/userSegment/Builder.vue'),
    meta: { menuHidden: true, title: '分层规则构建', group: 'customer', icon: 'SetUp', requiresAuth: true }
  }
]
