export default [
  {
    path: '/workflow-orchestrator/list',
    name: 'WorkflowOrchestratorList',
    component: () => import('@/views/workflowOrchestrator/List.vue'),
    meta: { menuKey: 'workflowOrchestrator', menuSection: 'reach', menuOrder: 15, menuRoles: ["admin","manager"], menuIcon: 'Share', title: '工作流编排', group: 'reach', icon: 'Share', requiresAuth: true }
  },
  {
    path: '/workflow-orchestrator/editor/:workflow_id',
    name: 'WorkflowOrchestratorEditor',
    component: () => import('@/views/workflowOrchestrator/Editor.vue'),
    meta: { menuHidden: true, title: '工作流编辑', group: 'reach', requiresAuth: true, hideInMenu: true }
  },
  {
    path: '/workflow-orchestrator/execution/:id',
    name: 'WorkflowOrchestratorExecution',
    component: () => import('@/views/workflowOrchestrator/Execution.vue'),
    meta: { menuHidden: true, title: '执行详情', group: 'reach', requiresAuth: true, hideInMenu: true }
  }
]
