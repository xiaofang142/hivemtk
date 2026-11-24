export default [
  {
    path: '/customerImportJob/list',
    name: 'CustomerImportJobList',
    component: () => import('@/views/customerImportJob/List.vue'),
    meta: { title: '客户导入任务', group: 'customer', icon: 'UploadFilled', requiresAuth: true }
  }
]
