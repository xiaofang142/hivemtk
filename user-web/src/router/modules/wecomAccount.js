export default [
  {
    path: '/wecomAccount/list',
    name: 'WecomAccountList',
    component: () => import('@/views/wecomAccount/List.vue'),
    meta: { menuKey: 'wecomAccount', menuSection: 'workspace', menuOrder: 10, menuRoles: ["admin","manager"], menuIcon: 'Connection', title: '企微账号管理', group: 'workspace', icon: 'Connection' }
  },
  {
    path: '/wecomAccount/data',
    name: 'WecomAccountData',
    component: () => import('@/views/wecomAccount/Data.vue'),
    meta: { menuHidden: true, title: '企微数据看板', group: 'workspace', icon: 'DataLine' }
  }
]
