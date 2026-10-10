export default [{
  path: '/asset-bundle/list',
  name: 'AssetBundleList',
  component: () => import('@/views/assetBundle/List.vue'),
  meta: { menuKey: 'assetBundleList', menuSection: 'system', menuParents: ["assetBundle"], menuOrder: 3, menuIcon: 'List', title: '资产包管理', group: 'aiAgent', icon: 'Files', requiresAuth: true }
}, {
  path: '/asset-bundle/playground',
  name: 'AssetBundlePlayground',
  component: () => import('@/views/assetBundle/Playground.vue'),
  meta: { menuKey: 'assetBundlePlayground', menuSection: 'system', menuParents: ["assetBundle"], menuOrder: 4, menuIcon: 'EditPen', title: '开发者 Playground', group: 'aiAgent', icon: 'EditPen', requiresAuth: true }
}, {
  path: '/asset-bundle/playground/:aid',
  name: 'AssetBundlePlaygroundEdit',
  component: () => import('@/views/assetBundle/Playground.vue'),
  meta: { menuHidden: true, title: '开发者 Playground', group: 'aiAgent', requiresAuth: true, hidden: true }
}, {
  path: '/asset-bundle/merchant-new',
  name: 'AssetBundleMerchantNew',
  component: () => import('@/views/assetBundle/MerchantEditor.vue'),
  meta: { menuKey: 'assetBundleMerchant', menuSection: 'system', menuParents: ["assetBundle"], menuOrder: 5, menuIcon: 'ChatDotRound', title: '商户新建话术包', group: 'aiAgent', icon: 'MagicStick', requiresAuth: true }
}, {
  path: '/asset-bundle/merchant/:aid',
  name: 'AssetBundleMerchantEdit',
  component: () => import('@/views/assetBundle/MerchantEditor.vue'),
  meta: { menuHidden: true, title: '商户配置话术包', group: 'aiAgent', requiresAuth: true, hidden: true }
}];
