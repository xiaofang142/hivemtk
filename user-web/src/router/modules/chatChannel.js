export default [{
  path: '/chatChannel/list',
  name: 'ChatChannelList',
  component: () => import('@/views/chatChannel/List.vue'),
  meta: { menuKey: 'chatChannelList', menuSection: 'aiAgent', menuParents: ["agentChannel"], menuOrder: 1, menuIcon: 'Connection', title: '客服渠道', group: 'customer', icon: 'Connection', requiresAuth: true }
}, {
  path: '/chatChannel/create',
  name: 'ChatChannelCreate',
  component: () => import('@/views/chatChannel/Create.vue'),
  meta: { menuHidden: true, title: '新建客服渠道', group: 'customer', icon: 'Plus', requiresAuth: true }
}, {
  path: '/chatChannel/edit/:id',
  name: 'ChatChannelEdit',
  component: () => import('@/views/chatChannel/Edit.vue'),
  meta: { menuHidden: true, title: '编辑客服渠道', group: 'customer', icon: 'Edit', requiresAuth: true }
}, {
  path: '/chatChannel/install-guide/:id?',
  name: 'ChatChannelInstallGuide',
  component: () => import('@/views/chatChannel/InstallGuide.vue'),
  meta: { menuHidden: true, title: 'Widget 安装引导', group: 'customer', icon: 'Guide', requiresAuth: true }
}];
