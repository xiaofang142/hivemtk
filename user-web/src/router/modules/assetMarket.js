export default [
  {
    path: '/asset-market',
    name: 'AssetMarket',
    component: () => import('@/views/assetMarket/Market.vue'),
    meta: { title: '资产市场', group: 'aiAgent', icon: 'Shop', requiresAuth: true }
  },
  {
    path: '/asset-market/detail/:id',
    name: 'AssetMarketDetail',
    component: () => import('@/views/assetMarket/Detail.vue'),
    meta: { title: '资产详情', group: 'aiAgent', requiresAuth: true, hidden: true }
  },
  {
    path: '/asset-market/my-assets',
    name: 'AssetMyAssets',
    component: () => import('@/views/assetMarket/MyAssets.vue'),
    meta: { title: '我的资产', group: 'aiAgent', icon: 'FolderOpened', requiresAuth: true }
  },
  {
    path: '/asset-market/sync-log',
    name: 'AssetSyncLog',
    component: () => import('@/views/assetMarket/SyncLog.vue'),
    meta: { title: '同步日志', group: 'aiAgent', icon: 'Refresh', requiresAuth: true }
  }
]
