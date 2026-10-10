export default [
  {
    path: '/xianyuCard',
    name: 'XianyuCard',
    component: () => import('@/views/xianyuCard/List.vue'),
    meta: { menuKey: 'xianyuCard', menuSection: 'reach', menuParents: ["xianyu"], menuOrder: 1, menuIcon: 'ShoppingBag', title: '闲鱼卡片', group: 'community', icon: 'ShoppingBag' }
  },
  {
    path: '/xianyu/stats',
    name: 'XianyuStats',
    component: () => import('@/views/xianyuCard/Stats.vue'),
    meta: { menuKey: 'xianyuStats', menuSection: 'reach', menuParents: ["xianyu"], menuOrder: 2, menuIcon: 'DataAnalysis', title: '闲鱼卡片统计', group: 'community', icon: 'DataAnalysis' }
  },
  {
    path: '/xianyu-card-stats/:id',
    name: 'XianyuCardStats',
    component: () => import('@/views/xianyuCard/CardStats.vue'),
    meta: { menuHidden: true, title: '闲鱼卡片详情统计', group: 'community', icon: 'DataAnalysis' }
  }
]