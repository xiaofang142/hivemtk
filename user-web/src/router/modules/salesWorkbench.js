// 销售工作台路由。单条路径 /sales-workbench，与既有 /sales-cockpit（AI 运维
// 聚合的驾驶舱）刻意分开 —— 两边是两套域：那边读 /api/ai/sales-cockpit 的
// ReAct/SOP/RAG 健康度，这边读 /api/sales-workbench/* 的销售待办与团队业绩，
// 混进一页会让"没数据"和"没装配"两种告警互相遮蔽。
export default [{
  path: '/sales-workbench',
  name: 'SalesWorkbenchIndex',
  component: () => import('@/views/salesWorkbench/Index.vue'),
  meta: { title: '销售工作台', group: 'workspace', icon: 'Notebook', requiresAuth: true }
}];
