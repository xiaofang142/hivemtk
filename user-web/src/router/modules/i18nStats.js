export default [{
  path: '/i18n/dashboard',
  name: 'I18nDashboard',
  component: () => import('@/views/i18n/Dashboard.vue'),
  meta: { menuKey: 'i18nDashboard', menuSection: 'knowledge', menuParents: ["i18nManage"], menuOrder: 2, menuRoles: ["admin","manager"], menuIcon: 'DataLine', title: '多语言监控', group: 'i18n', icon: 'DataLine', requiresAuth: true }
}];
