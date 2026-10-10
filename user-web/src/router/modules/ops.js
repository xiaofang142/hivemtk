export default [
  {
    path: '/ops-overview',
    name: 'OpsOverview',
    component: () => import('@/views/OpsOverview/Index.vue'),
    meta: { menuKey: 'opsOverview', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 13, menuRoles: ["admin"], menuIcon: 'Monitor', title: '运维总览', icon: 'DataAnalysis', requiresAdmin: true, keepAlive: true }
  },
  {
    path: '/sales-cockpit',
    name: 'SalesCockpitIndex',
    component: () => import('@/views/SalesCockpit/Index.vue'),
    meta: { menuKey: 'salesCockpit', menuSection: 'workspace', menuOrder: 6, menuRoles: ["admin","manager","sales"], menuIcon: 'DataLine', title: 'AI 销冠驾驶舱', icon: 'TrendCharts', keepAlive: true }
  }
]
