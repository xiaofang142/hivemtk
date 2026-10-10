export default [{
  path: '/system/users',
  name: 'SystemUserList',
  component: () => import('@/views/system/UserList.vue'),
  meta: { menuKey: 'systemUser', menuSection: 'system', menuParents: ["teamGroup"], menuOrder: 1, menuIcon: 'UserFilled',
    title: 'systemUser.title',
    group: 'system',
    icon: 'UserFilled',
    requiresAuth: true,
    requiresAdmin: true
  }
}];
