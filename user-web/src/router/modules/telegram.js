export default [
  {
    path: '/telegram',
    name: 'Telegram',
    component: () => import('@/views/telegram/account.vue'),
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
