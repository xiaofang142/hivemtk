export default [
  {
    path: '/email',
    name: 'Email',
    component: () => import('@/views/email/EmailList.vue'),
    meta: { title: '邮件列表', group: 'reach', icon: 'ChatSquare' }
  },
  {
    path: '/email/drafts',
    name: 'EmailDrafts',
    component: () => import('@/views/email/Drafts.vue'),
    meta: { title: '我的草稿', group: 'reach', icon: 'Document' }
  },
  {
    path: '/email/jobs',
    name: 'EmailJobs',
    component: () => import('@/views/email/Jobs.vue'),
    meta: { title: '我的任务', group: 'reach', icon: 'Document' }
  },
  {
    path: '/email/smtp',
    name: 'EmailSmtp',
    component: () => import('@/views/email/Smtp.vue'),
    meta: { title: '邮件账号', group: 'reach', icon: 'Setting' }
  },
  {
    path: '/email/info',
    name: 'EmailInfo',
    component: () => import('@/views/email/Info.vue'),
    meta: { title: '邮件代理', group: 'reach', icon: 'Setting' }
  },
  {
    path: '/email/guide',
    name: 'EmailGuide',
    component: () => import('@/views/email/Guide.vue'),
    meta: { title: '使用引导', group: 'reach', icon: 'Setting' }
  },
  {
    path: '/email/deliverability',
    name: 'EmailDeliverability',
    component: () => import('@/views/email/Deliverability.vue'),
    meta: { title: '送达分析', group: 'email', icon: 'DataAnalysis', requiresAuth: true }
  },
  {
    path: '/email/drag-editor',
    name: 'EmailDragEditor',
    component: () => import('@/views/email/DragEditor.vue'),
    meta: { title: '拖拽编辑器', group: 'reach', icon: 'EditPen', requiresAuth: true }
  }
]