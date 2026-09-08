export default [
  {
    path: '/qq',
    name: 'QQBot',
    component: () => import('@/views/qq/account.vue'),
    meta: { title: 'QQ 机器人', group: 'community', icon: 'ChatDotRound' }
  },
  {
    path: '/qq/account',
    name: 'QQAccount',
    component: () => import('@/views/qq/account.vue'),
    meta: { title: '机器人账号', group: 'community', icon: 'Cpu' }
  }
]
