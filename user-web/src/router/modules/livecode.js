export default [
  {
    path: '/livecode',
    name: 'LiveCodeManagement',
    component: () => import('@/views/livecode/LiveCodeManagement.vue'),
    meta: { menuKey: 'livecode', menuSection: 'reach', menuOrder: 14, menuIcon: 'QrCode', 
      title: '活码管理'
    }
  }
]