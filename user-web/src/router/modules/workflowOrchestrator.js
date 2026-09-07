export default [
  {
    path: '/workflow-orchestrator/list',
    name: 'WorkflowOrchestratorList',
    component: () => import('@/views/workflowOrchestrator/List.vue'),
    meta: { title: '工作流编排', group: 'reach', icon: 'Share', requiresAuth: true }
  },
  {
    path: '/workflow-orchestrator/editor/:workflow_id',
    name: 'WorkflowOrchestratorEditor',
    component: () => import('@/views/workflowOrchestrator/Editor.vue'),
    meta: { title: '工作流编辑', group: 'reach', requiresAuth: true, hideInMenu: true }
  },
  {
    path: '/workflow-orchestrator/execution/:id',
    name: 'WorkflowOrchestratorExecution',
    component: () => import('@/views/workflowOrchestrator/Execution.vue'),
    meta: { title: '执行详情', group: 'reach', requiresAuth: true, hideInMenu: true }
  }
]
