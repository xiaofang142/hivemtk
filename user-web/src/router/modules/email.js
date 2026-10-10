export default [
  {
    path: '/email',
    name: 'Email',
    component: () => import('@/views/email/EmailList.vue'),
    meta: { menuKey: 'emailList', menuSection: 'reach', menuParents: ["emailReach"], menuOrder: 1, menuIcon: 'ChatSquare', title: '邮件列表', group: 'reach', icon: 'ChatSquare' }
  },
  {
    path: '/email/drafts',
    name: 'EmailDrafts',
    component: () => import('@/views/email/Drafts.vue'),
    meta: { menuKey: 'emailDrafts', menuSection: 'reach', menuParents: ["emailReach"], menuOrder: 2, menuIcon: 'Document', title: '我的草稿', group: 'reach', icon: 'Document' }
  },
  {
    path: '/email/jobs',
    name: 'EmailJobs',
    component: () => import('@/views/email/Jobs.vue'),
    meta: { menuKey: 'emailJobs', menuSection: 'reach', menuParents: ["emailReach"], menuOrder: 3, menuIcon: 'Document', title: '我的任务', group: 'reach', icon: 'Document' }
  },
  {
    path: '/email/smtp',
    name: 'EmailSmtp',
    component: () => import('@/views/email/Smtp.vue'),
    meta: { menuKey: 'emailSmtp', menuSection: 'reach', menuParents: ["emailReach"], menuOrder: 4, menuIcon: 'Setting', title: '邮件账号', group: 'reach', icon: 'Setting' }
  },
  {
    path: '/email/info',
    name: 'EmailInfo',
    component: () => import('@/views/email/Info.vue'),
    meta: { menuKey: 'emailInfo', menuSection: 'reach', menuParents: ["emailReach"], menuOrder: 5, menuIcon: 'Setting', title: '邮件代理', group: 'reach', icon: 'Setting' }
  },
  {
    path: '/email/guide',
    name: 'EmailGuide',
    component: () => import('@/views/email/Guide.vue'),
    meta: { menuKey: 'emailGuide', menuSection: 'reach', menuParents: ["emailReach"], menuOrder: 7, menuIcon: 'Document', title: '使用引导', group: 'reach', icon: 'Setting' }
  },
  {
    path: '/email/deliverability',
    name: 'EmailDeliverability',
    component: () => import('@/views/email/Deliverability.vue'),
    meta: { menuKey: 'emailDeliverability', menuSection: 'reach', menuParents: ["emailReach"], menuOrder: 6, menuIcon: 'DataAnalysis', title: '送达分析', group: 'email', icon: 'DataAnalysis', requiresAuth: true }
  },
  {
    path: '/email/drag-editor',
    name: 'EmailDragEditor',
    component: () => import('@/views/email/DragEditor.vue'),
    meta: { menuHidden: true, title: '拖拽编辑器', group: 'reach', icon: 'EditPen', requiresAuth: true }
  }
]