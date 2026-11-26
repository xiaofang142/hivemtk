export default [
  // T04：同一页面的重复别名路径收口为重定向，菜单与深链只认 /telegram/account。
  {
    path: '/telegram',
    name: 'Telegram',
    redirect: '/telegram/account',
    meta: { title: 'TG 机器人', group: 'community', icon: 'ChatDotRound' }
  },
  {
    path: '/telegram/account',
    name: 'TelegramAccount',
    component: () => import('@/views/telegram/account.vue'),
    meta: { title: '机器人账号', group: 'community', icon: 'Cpu' }
  },
  {
    path: '/telegram/gate',
    name: 'TelegramGate',
    component: () => import('@/views/telegram/gate.vue'),
    meta: { title: '入群管控', group: 'community', icon: 'Lock' }
  }
]
