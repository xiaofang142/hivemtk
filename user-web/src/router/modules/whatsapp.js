export default [
  {
    path: '/whatsapp',
    name: 'Whatsapp',
    component: () => import('@/views/whatsapp/WhatsappAccount.vue'),
    meta: { menuHidden: true, title: 'WhatsApp 社群', group: 'community', icon: 'ChatDotRound' }
  },
  {
    path: '/whatsapp/account',
    name: 'WhatsappAccount',
    component: () => import('@/views/whatsapp/WhatsappAccount.vue'),
    meta: { menuKey: 'whatsappAccount', menuSection: 'reach', menuParents: ["whatsapp"], menuOrder: 1, menuIcon: 'Cpu', title: '账号管理', group: 'community', icon: 'Cpu' }
  },
  {
    path: '/whatsapp/drafts',
    name: 'WhatsappDrafts',
    component: () => import('@/views/whatsapp/WhatsappDrafts.vue'),
    meta: { menuKey: 'whatsappDrafts', menuSection: 'reach', menuParents: ["whatsapp"], menuOrder: 2, menuIcon: 'Document', title: '草稿箱', group: 'community', icon: 'Document' }
  },
  {
    path: '/whatsapp/jobs',
    name: 'WhatsappJobs',
    component: () => import('@/views/whatsapp/WhatsappJobs.vue'),
    meta: { menuKey: 'whatsappJobs', menuSection: 'reach', menuParents: ["whatsapp"], menuOrder: 3, menuIcon: 'Promotion', title: '群发', group: 'community', icon: 'Promotion' }
  },
  {
    path: '/whatsapp/lead-group-selection',
    name: 'LeadGroupSelection',
    component: () => import('@/views/whatsappBot/LeadGroupSelection.vue'),
    meta: { menuKey: 'whatsappLeadGroup', menuSection: 'reach', menuParents: ["whatsapp"], menuOrder: 4, menuIcon: 'UserFilled', title: '从线索库选择群体', group: 'community', icon: 'User', requiresAuth: true }
  },
  {
    path: '/whatsapp/group-messaging',
    name: 'BulkMessaging',
    component: () => import('@/views/whatsappBot/BulkMessaging.vue'),
    meta: { menuKey: 'whatsappGroupMessaging', menuSection: 'reach', menuParents: ["whatsapp"], menuOrder: 6, menuIcon: 'Grid', title: '批量消息发送', group: 'community', icon: 'ChatLineRound', requiresAuth: true }
  },
  {
    path: '/whatsapp/bulk-matrix',
    name: 'WhatsappBulkMatrix',
    component: () => import('@/views/whatsapp/BulkMatrix.vue'),
    meta: { menuKey: 'whatsappBulkMatrix', menuSection: 'reach', menuParents: ["whatsapp"], menuOrder: 5, menuIcon: 'Grid', title: '群发矩阵', group: 'community', icon: 'Grid', requiresAuth: true }
  }
]