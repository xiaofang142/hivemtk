export default [
  {
    path: '/tiktok',
    name: 'TikTok',
    redirect: '/tiktok/list',
    meta: { menuHidden: true, title: 'TikTok', group: 'reach', icon: 'VideoPlay' }
  },
  {
    path: '/tiktok/list',
    name: 'TikTokList',
    component: () => import('@/views/tiktokCard/List.vue'),
    meta: { menuKey: 'tiktokList', menuSection: 'reach', menuParents: ["tiktok"], menuOrder: 1, menuIcon: 'VideoPlay', title: 'TikTok卡片', group: 'reach', icon: 'VideoPlay', requiresAuth: true }
  },
  {
    path: '/tiktok/stats',
    name: 'TikTokStats',
    component: () => import('@/views/tiktokCard/Stats.vue'),
    meta: { menuKey: 'tiktokStats', menuSection: 'reach', menuParents: ["tiktok"], menuOrder: 2, menuIcon: 'DataAnalysis', title: 'TikTok统计', group: 'reach', icon: 'DataAnalysis', requiresAuth: true }
  },
  {
    path: '/tiktok-card-stats/:id',
    name: 'TikTokCardStats',
    component: () => import('@/views/tiktokCard/CardStats.vue'),
    meta: { menuHidden: true, title: 'TikTok卡片统计', group: 'reach', icon: 'DataAnalysis', requiresAuth: true },
    props: true
  }
]
