export default [{
  // T04：同一页面的重复别名路径收口为重定向，菜单与深链只认 /feishu/account。
  path: '/feishu',
  name: 'Feishu',
  redirect: '/feishu/account',
  meta: { title: '飞书', group: 'community', icon: 'ChatLineRound' }
}, {
  path: '/feishu/account',
  name: 'FeishuAccount',
  component: () => import('@/views/feishu/FeishuAccount.vue'),
  meta: { title: '飞书账号', group: 'community', icon: 'Cpu' }
}];
