export default [
  {
    path: '/telegram',
    name: 'Telegram',
    component: () => import('@/views/telegram/account.vue'),
    meta: { menuHidden: true, title: 'TG 机器人', group: 'community', icon: 'ChatDotRound' }
  },
  {
    path: '/telegram/account',
    name: 'TelegramAccount',
    component: () => import('@/views/telegram/account.vue'),
    meta: { menuKey: 'telegramAccount', menuSection: 'reach', menuParents: ["telegram"], menuOrder: 1, menuIcon: 'Cpu', title: '机器人账号', group: 'community', icon: 'Cpu' }
  },
  {
    path: '/telegram/gate',
    name: 'TelegramGate',
    component: () => import('@/views/telegram/gate.vue'),
    meta: { menuHidden: true, title: '入群管控', group: 'community', icon: 'Lock' }
  }
]
