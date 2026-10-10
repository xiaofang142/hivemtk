export default [
  {
    path: '/backup/list',
    name: 'BackupList',
    component: () => import('@/views/backup/List.vue'),
    meta: { menuKey: 'backupList', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 8, menuIcon: 'FolderOpened', title: '备份恢复', group: 'system', icon: 'FolderOpened', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/backup/enhanced',
    name: 'BackupEnhanced',
    component: () => import('@/views/backup/Enhanced.vue'),
    meta: { menuKey: 'backupEnhanced', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 9, menuIcon: 'FolderChecked', title: '备份管理', group: 'system', icon: 'Box', requiresAuth: true, requiresAdmin: true }
  }
]
