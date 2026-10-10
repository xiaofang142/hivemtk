export default [{
  path: '/system/permissions',
  name: 'PermissionPanel',
  component: () => import('@/views/system/PermissionPanel.vue'),
  meta: { menuKey: 'permissionManage', menuSection: 'system', menuParents: ["teamGroup"], menuOrder: 3, menuIcon: 'Lock',
    title: 'permission.title',
    group: 'system',
    icon: 'Lock',
    requiresAuth: true,
    requiresAdmin: true
  }
}];
