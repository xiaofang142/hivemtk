export default [
  {
    path: '/xiaohongshuCard',
    name: 'XiaohongshuCard',
    component: () => import('@/views/xiaohongshuCard/List.vue'),
    meta: { menuKey: 'xiaohongshuCard', menuSection: 'reach', menuParents: ["xiaohongshu"], menuOrder: 1, menuIcon: 'Picture',
      title: '小红书卡片',
      group: 'community',
      icon: 'Picture'
    }
  },
  {
    path: '/xiaohongshu/stats',
    name: 'XiaohongshuStats',
    component: () => import('@/views/xiaohongshuCard/Stats.vue'),
    meta: { menuKey: 'xiaohongshuStats', menuSection: 'reach', menuParents: ["xiaohongshu"], menuOrder: 2, menuIcon: 'DataAnalysis',
      title: '小红书卡片统计',
      group: 'community',
      icon: 'DataAnalysis'
    }
  },
  {
    path: '/xiaohongshu-card-stats/:id',
    name: 'XiaohongshuCardStats',
    component: () => import('@/views/xiaohongshuCard/CardStats.vue'),
    meta: { menuHidden: true,
      title: '小红书卡片详情统计',
      group: 'community',
      icon: 'DataAnalysis'
    }
  }
]
