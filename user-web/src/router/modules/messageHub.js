export default [
  {
    path: '/messageHub/list',
    name: 'MessageHubList',
    component: () => import('@/views/messageHub/List.vue'),
    meta: { menuKey: 'messageHub', menuSection: 'workspace', menuOrder: 1, menuRoles: ["admin","manager"], menuIcon: 'MessageBox', title: '消息中台', group: 'workspace', icon: 'MessageBox' }
  },
  {
    path: '/messageHub/dashboard',
    name: 'MessageHubDashboard',
    component: () => import('@/views/messageHub/Dashboard.vue'),
    meta: { menuKey: 'messageHubDashboard', menuSection: 'workspace', menuOrder: 2, menuRoles: ["admin","manager"], menuIcon: 'DataBoard', title: '消息总览', group: 'workspace', icon: 'DataBoard' }
  }
]
