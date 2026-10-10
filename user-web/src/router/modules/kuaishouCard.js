export default [
  {
    path: '/kuaishouCard',
    name: 'KuaishouCard',
    component: () => import('@/views/kuaishouCard/List.vue'),
    meta: { menuKey: 'kuaishouCard', menuSection: 'reach', menuParents: ["kuaishou"], menuOrder: 1, menuIcon: 'ChatDotRound', title: '快手卡片', group: 'community', icon: 'ChatDotRound' }
  },
  {
    path: '/kuaishou/stats',
    name: 'KuaishouStats',
    component: () => import('@/views/kuaishouCard/Stats.vue'),
    meta: { menuKey: 'kuaishouStats', menuSection: 'reach', menuParents: ["kuaishou"], menuOrder: 2, menuIcon: 'DataAnalysis', title: '快手卡片统计', group: 'community', icon: 'DataAnalysis' }
  },
  {
    path: '/kuaishou-card-stats/:id',
    name: 'KuaishouCardStats',
    component: () => import('@/views/kuaishouCard/CardStats.vue'),
    meta: { menuHidden: true, title: '快手卡片详情统计', group: 'community', icon: 'DataAnalysis' }
  }
]
