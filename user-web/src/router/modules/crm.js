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
  },
  {
    path: '/crm/contacts',
    name: 'CrmContacts',
    component: () => import('@/views/crm/Entities.vue'),
    meta: { title: '客户联系人', group: 'customer', icon: 'UserFilled', requiresAuth: true, entityType: 'contact' }
  },
  {
    path: '/crm/suppliers',
    name: 'CrmSuppliers',
    component: () => import('@/views/crm/Entities.vue'),
    meta: { title: '客户供应商', group: 'customer', icon: 'Goods', requiresAuth: true, entityType: 'supplier' }
  },
  {
    path: '/crm/service-providers',
    name: 'CrmServiceProviders',
    component: () => import('@/views/crm/Entities.vue'),
    meta: { title: '客户服务商', group: 'customer', icon: 'Tools', requiresAuth: true, entityType: 'serviceProvider' }
  },
  {
    path: '/crm/companies',
    name: 'CrmCompanies',
    component: () => import('@/views/crm/Companies.vue'),
    meta: { title: '公司主数据', group: 'customer', icon: 'OfficeBuilding', requiresAuth: true }
  }
]
