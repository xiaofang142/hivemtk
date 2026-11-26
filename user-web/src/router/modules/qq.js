export default [
  // T04：同一页面的重复别名路径收口为重定向，菜单与深链只认 /qq/account。
  {
    path: '/qq',
    name: 'QQBot',
    redirect: '/qq/account',
    meta: { title: 'QQ 机器人', group: 'community', icon: 'ChatDotRound' }
  },
  {
    path: '/qq/account',
    name: 'QQAccount',
    component: () => import('@/views/qq/account.vue'),
    meta: { title: '机器人账号', group: 'community', icon: 'Cpu' }
  }
]
