const loadList = () => import('@/views/assetBundle/List.vue')
const loadEditor = () => import('@/views/assetBundle/MerchantEditor.vue')
const listPath = '/asset-bundle/list'

export default [{
  path: '/asset-bundle/list',
  name: 'AssetBundleList',
  component: loadList,
  meta: { title: '资产包管理', group: 'aiAgent', icon: 'Files', requiresAuth: true }
}, {
  path: '/asset-bundle/playground',
  name: 'AssetBundlePlayground',
  component: () => import('@/views/assetBundle/Playground.vue'),
  meta: { title: '开发者 Playground', group: 'aiAgent', icon: 'EditPen', requiresAuth: true }
}, {
  path: '/asset-bundle/playground/:aid',
  name: 'AssetBundlePlaygroundEdit',
  component: () => import('@/views/assetBundle/Playground.vue'),
  meta: { title: '开发者 Playground', group: 'aiAgent', requiresAuth: true, hidden: true }
}, {
  path: '/asset-bundle/merchant-new',
  name: 'AssetBundleMerchantNew',
  components: { default: loadEditor, list: loadList },
  meta: { title: '商户新建话术包', group: 'aiAgent', icon: 'MagicStick', requiresAuth: true, listEditor: listPath }
}, {
  path: '/asset-bundle/merchant/:aid',
  name: 'AssetBundleMerchantEdit',
  components: { default: loadEditor, list: loadList },
  meta: { title: '商户配置话术包', breadcrumbKey: 'navigation.configureMerchantBundle', group: 'aiAgent', requiresAuth: true, hidden: true, listEditor: listPath }
}];
