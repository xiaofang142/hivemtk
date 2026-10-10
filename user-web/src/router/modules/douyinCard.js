export default [
  {
    path: '/douyinCard',
    name: 'DouyinCard',
    component: () => import('@/views/douyinCard/List.vue'),
    meta: { menuKey: 'douyinCard', menuSection: 'reach', menuParents: ["douyin"], menuOrder: 1, menuIcon: 'VideoPlay', title: '抖音卡片', group: 'community', icon: 'VideoPlay' }
  },
  {
    path: '/douyin/stats',
    name: 'DouyinStats',
    component: () => import('@/views/douyinCard/Stats.vue'),
    meta: { menuKey: 'douyinStats', menuSection: 'reach', menuParents: ["douyin"], menuOrder: 2, menuIcon: 'DataAnalysis', title: '抖音卡片统计', group: 'community', icon: 'DataAnalysis' }
  },
  {
    path: '/douyin-card-stats/:id',
    name: 'DouyinCardStats',
    component: () => import('@/views/douyinCard/CardStats.vue'),
    meta: { menuHidden: true, title: '抖音卡片详情统计', group: 'community', icon: 'DataAnalysis' }
  }
]
