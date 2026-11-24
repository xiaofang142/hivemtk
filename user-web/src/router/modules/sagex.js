export default [
  {
    path: '/sagex/licenses',
    name: 'SageXLicenseList',
    component: () => import('@/views/sagex/LicenseList.vue'),
    meta: { title: 'SageX 授权码', group: 'system', icon: 'Key' }
  }
]