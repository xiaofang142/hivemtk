export default [
  {
    path: '/domainPool',
    name: 'DomainPool',
    component: () => import('@/views/domainPool/List.vue'),
    meta: { menuKey: 'domainPool', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 7, menuIcon: 'Link', title: '域名池管理', group: 'system', icon: 'Link', requiresAuth: true, requiresAdmin: true }
  }
]