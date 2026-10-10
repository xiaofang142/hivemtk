export default [{
  path: '/bridge',
  name: 'BridgeRoot',
  redirect: '/bridge/token',
  meta: { menuHidden: true, title: '渠道接入', icon: 'Link', requiresAdmin: true }
}, {
  path: '/bridge/token',
  name: 'BridgeTokenManagement',
  component: () => import('@/views/bridge/TokenManagement.vue'),
  meta: { menuKey: 'bridgeToken', menuSection: 'system', menuParents: ["permissionGroup"], menuOrder: 3, menuIcon: 'Key', title: '桥接凭证', requiresAdmin: true }
}, {
  path: '/token',
  redirect: '/bridge/token'
}];
