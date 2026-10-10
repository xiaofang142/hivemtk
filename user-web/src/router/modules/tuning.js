export default [{
  path: '/confidence/panel',
  name: 'ConfidencePanel',
  component: () => import('@/views/confidence/Panel.vue'),
  meta: { menuKey: 'confidencePanel', menuSection: 'analytics', menuOrder: 9, menuRoles: ["admin","manager","viewer"], menuIcon: 'CircleCheck', title: '置信度运营', group: 'aiAgent', icon: 'TrendCharts', requiresAuth: true }
}, {
  path: '/humanize/panel',
  name: 'HumanizePanel',
  component: () => import('@/views/humanize/Panel.vue'),
  meta: { menuKey: 'humanizePanel', menuSection: 'analytics', menuOrder: 10, menuRoles: ["admin","manager","viewer"], menuIcon: 'MagicStick', title: '拟人度评估', group: 'aiAgent', icon: 'UserFilled', requiresAuth: true }
}, {
  path: '/feedbackLoop/panel',
  name: 'FeedbackLoopPanel',
  component: () => import('@/views/feedbackLoop/Panel.vue'),
  meta: { menuKey: 'feedbackLoopPanel', menuSection: 'analytics', menuOrder: 11, menuRoles: ["admin","manager","viewer"], menuIcon: 'Refresh', title: '反馈学习闭环', group: 'aiAgent', icon: 'Connection', requiresAuth: true }
}];
