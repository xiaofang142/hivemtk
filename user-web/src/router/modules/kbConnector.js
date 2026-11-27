export default [
  {
    // 2026-11-27 菜单审计：与 /knowledge/connectors 同属「外部知识源」，
    // 由 ExternalSources.vue 提供 tab 容器；本 path 保留为第二个 tab 的深链。
    path: '/kb/connectors',
    name: 'KbConnectors',
    component: () => import('@/views/KnowledgeWorkspace/ExternalSources.vue'),
    meta: { title: '外部知识源', group: 'knowledge', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/cap/matrix',
    name: 'CapabilityMatrix',
    component: () => import('@/views/cap/Matrix.vue'),
    meta: { title: '能力矩阵', group: 'system', icon: 'Grid', requiresAuth: true }
  },
  {
    // D01 能力运行诊断：implemented / configured / ready 三层视图
    path: '/cap/diagnostics',
    name: 'CapabilityDiagnostics',
    component: () => import('@/views/cap/Diagnostics.vue'),
    meta: { title: '能力运行诊断', group: 'system', icon: 'DataAnalysis', requiresAuth: true }
  }
]
