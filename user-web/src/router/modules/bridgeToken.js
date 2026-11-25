export default [{
  path: '/bridge',
  name: 'BridgeRoot',
  redirect: '/channels/identify-codes',
  meta: { title: '渠道接入', icon: 'Link', requiresAdmin: true }
}, {
  path: '/bridge/token',
  name: 'BridgeTokenManagement',
  redirect: '/channels/identify-codes',
  meta: { title: '桥接凭证', requiresAdmin: true }
}, {
  path: '/bridge/extension',
  name: 'BridgeExtensionPackage',
  component: () => import('@/views/bridge/ExtensionPackage.vue'),
  meta: { title: '扩展安装包', icon: 'Download', requiresAuth: true }
}, {
  path: '/token',
  redirect: '/channels/identify-codes',
  meta: { requiresAdmin: true }
}];
