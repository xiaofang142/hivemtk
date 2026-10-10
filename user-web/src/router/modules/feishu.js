export default [{
  path: '/feishu',
  name: 'Feishu',
  component: () => import('@/views/feishu/FeishuAccount.vue'),
  meta: { menuHidden: true, title: '飞书', group: 'community', icon: 'ChatLineRound' }
}, {
  path: '/feishu/account',
  name: 'FeishuAccount',
  component: () => import('@/views/feishu/FeishuAccount.vue'),
  meta: { menuKey: 'feishuAccount', menuSection: 'reach', menuParents: ["feishu"], menuOrder: 1, menuIcon: 'Cpu', title: '飞书账号', group: 'community', icon: 'Cpu' }
}];
