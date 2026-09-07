export default [
  {
    path: '/ops-overview',
    name: 'OpsOverview',
    component: () => import('@/views/OpsOverview/Index.vue'),
    meta: { title: '运维总览', icon: 'DataAnalysis', requiresAdmin: true, keepAlive: true }
  },
  {
    path: '/sales-cockpit',
    name: 'SalesCockpitIndex',
    component: () => import('@/views/SalesCockpit/Index.vue'),
    meta: { title: 'AI 销冠驾驶舱', icon: 'TrendCharts', keepAlive: true }
  }
]
