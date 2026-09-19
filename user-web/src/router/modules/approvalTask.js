export default [
  {
    path: '/approvalTask/list',
    name: 'ApprovalTaskList',
    component: () => import('@/views/approvalTask/List.vue'),
    meta: { title: '待办中心', group: 'workspace', icon: 'Tickets' }
  }
]
