export default [
  {
    path: '/channels/accounts',
    name: 'ChannelAccountManage',
    component: () => import('@/views/channels/index.vue'),
    meta: { title: '渠道账号管理', group: 'channel', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/channels/manage',
    name: 'ChannelManage',
    component: () => import('@/views/channels/index.vue'),
    meta: { title: '渠道管理', group: 'channel', icon: 'Grid', requiresAuth: true }
  },
  {
    path: '/channels/identify-codes',
    name: 'ChannelIdentifyCodeManage',
    component: () => import('@/views/channels/index.vue'),
    meta: { title: '账号识别码管理', group: 'channel', icon: 'Key', requiresAuth: true, requiresAdmin: true }
  }
]
