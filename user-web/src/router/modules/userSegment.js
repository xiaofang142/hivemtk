export default [
  {
    path: '/userSegment/list',
    name: 'UserSegmentList',
    component: () => import('@/views/userSegment/List.vue'),
    meta: { title: '用户分层 RFM', group: 'customer', icon: 'PieChart', requiresAuth: true }
  },
  {
    path: '/userSegment/rfm-matrix',
    name: 'RfmMatrix',
    component: () => import('@/views/userSegment/RfmMatrix.vue'),
    meta: { title: 'RFM 矩阵', group: 'analytics', icon: 'Grid', requiresAuth: true }
  },
  {
    path: '/userSegment/builder',
    name: 'UserSegmentBuilder',
    component: () => import('@/views/userSegment/Builder.vue'),
    meta: { title: '分层规则构建', group: 'customer', icon: 'SetUp', requiresAuth: true }
  }
]
