export default [
  {
    path: '/backup/list',
    name: 'BackupList',
    component: () => import('@/views/backup/List.vue'),
    meta: { title: '备份恢复', group: 'system', icon: 'FolderOpened', requiresAuth: true, requiresAdmin: true }
  }
  // /backup/enhanced（备份高级版）已于 2026-11-10 下线：与 /backup/list 打的是
  // 同一组后端端点（/api/backup/* 别名组与 /api/backups 权威组包同一个
  // BackupService/RestoreService）。Enhanced 独有的备份策略编辑与恢复影响预览
  // 已并入 views/backup/List.vue；菜单节点 backupEnhanced 同步从注册表除名。
]
