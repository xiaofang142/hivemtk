export default [
  {
    path: '/crm/list',
    name: 'CrmCustomerList',
    component: () => import('@/views/crm/List.vue'),
    meta: { title: '客户主数据', group: 'customer', icon: 'User', requiresAuth: true }
  },
  {
    path: '/crm/dedupe',
    name: 'CrmDedupe',
    component: () => import('@/views/crm/Dedupe.vue'),
    meta: { title: '客户去重合并', group: 'customer', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/crm/profile360/:id?',
    name: 'CrmProfile360',
    component: () => import('@/views/crm/Profile360.vue'),
    meta: { title: '客户 360', group: 'customer', icon: 'View', requiresAuth: true }
  },
  {
    path: '/crm/import-export',
    name: 'CrmImportExport',
    component: () => import('@/views/crm/ImportExport.vue'),
    meta: { title: '客户导入导出', group: 'customer', icon: 'Sort', requiresAuth: true }
  }
]
