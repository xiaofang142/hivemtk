export default [{
  path: '/system/roles',
  name: 'RoleList',
  component: () => import('@/views/system/RoleList.vue'),
  meta: { menuKey: 'roleManage', menuSection: 'system', menuParents: ["teamGroup"], menuOrder: 2, menuIcon: 'UserFilled',
    title: 'role.title',
    group: 'system',
    icon: 'UserFilled',
    requiresAuth: true,
    requiresAdmin: true
  }
}];
