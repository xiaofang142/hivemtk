export default [
  {
    path: '/approvalTask/list',
    name: 'ApprovalTaskList',
    component: () => import('@/views/approvalTask/List.vue'),
    meta: { menuKey: 'approvalTask', menuSection: 'workspace', menuOrder: 4, menuIcon: 'Tickets', title: '待办中心', group: 'workspace', icon: 'Tickets' }
  }
]
