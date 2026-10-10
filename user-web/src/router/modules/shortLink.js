export default [
  {
    path: '/shortLink',
    name: 'ShortLink',
    component: () => import('@/views/shortLink/List.vue'),
    meta: { menuKey: 'shortLinkList', menuSection: 'reach', menuParents: ["shortLink"], menuOrder: 1, menuIcon: 'Link', title: '短链管理', group: 'community', icon: 'Link' }
  },
  {
    path: '/shortLink/stats',
    name: 'ShortLinkStats',
    component: () => import('@/views/shortLink/Stats.vue'),
    meta: { menuKey: 'shortLinkStats', menuSection: 'reach', menuParents: ["shortLink"], menuOrder: 2, menuIcon: 'DataAnalysis', title: '短链统计', group: 'community', icon: 'DataAnalysis' }
  }
]