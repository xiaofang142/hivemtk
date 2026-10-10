export default [
  {
    path: '/knowledge/management',
    name: 'KnowledgeManagement',
    component: () => import('@/views/KnowledgeWorkspace/KnowledgeManagement.vue'),
    meta: { menuKey: 'knowledgeManagement', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 1, menuIcon: 'Document', title: '知识库管理', group: 'knowledge', icon: 'Files', requiresAuth: true }
  },
  {
    path: '/knowledge/batch-import',
    name: 'KnowledgeBatchImport',
    component: () => import('@/views/KnowledgeWorkspace/BatchImport.vue'),
    meta: { menuKey: 'knowledgeBatchImport', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 2, menuIcon: 'UploadFilled', title: '批量导入', group: 'knowledge', icon: 'UploadFilled', requiresAuth: true }
  },
  {
    path: '/knowledge/playground',
    name: 'KnowledgePlayground',
    component: () => import('@/views/KnowledgeWorkspace/Playground.vue'),
    meta: { menuKey: 'knowledgePlayground', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 3, menuIcon: 'Aim', title: '检索 Playground', group: 'knowledge', icon: 'Aim', requiresAuth: true }
  },
  {
    path: '/knowledge/connectors',
    name: 'KnowledgeConnectors',
    component: () => import('@/views/KnowledgeWorkspace/Connectors.vue'),
    meta: { menuHidden: true, title: '外部连接器', group: 'knowledge', icon: 'Link', requiresAuth: true }
  },
  {
    path: '/knowledge/feedbacks',
    name: 'KnowledgeFeedbacks',
    component: () => import('@/views/KnowledgeWorkspace/FeedbackList.vue'),
    meta: { menuKey: 'knowledgeFeedbacks', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 4, menuIcon: 'ChatLineRound', title: '反馈管理', group: 'knowledge', icon: 'ChatLineRound', requiresAuth: true }
  },
  {
    path: '/knowledge/tokens',
    name: 'KnowledgeTokens',
    component: () => import('@/views/KnowledgeWorkspace/ApiToken.vue'),
    meta: { menuKey: 'knowledgeTokens', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 5, menuIcon: 'Key', title: 'API Token', group: 'knowledge', icon: 'Key', requiresAuth: true }
  },
  {
    path: '/knowledge/external',
    name: 'KnowledgeExternal',
    component: () => import('@/views/KnowledgeWorkspace/ExternalImport.vue'),
    meta: { menuKey: 'knowledgeExternal', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 6, menuIcon: 'Connection', title: '外部系统接入', group: 'knowledge', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/knowledge/statistics',
    name: 'KnowledgeStatistics',
    component: () => import('@/views/KnowledgeWorkspace/KnowledgeStatistics.vue'),
    meta: { menuKey: 'knowledgeStatistics', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 7, menuIcon: 'DataAnalysis', title: '知识库统计', group: 'knowledge', icon: 'DataAnalysis', requiresAuth: true }
  },
  {
    path: '/knowledge/openapi',
    name: 'KnowledgeOpenAPI',
    component: () => import('@/views/KnowledgeWorkspace/OpenAPIIntegration.vue'),
    meta: { menuKey: 'knowledgeOpenAPI', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 9, menuIcon: 'Connection', title: 'OpenAPI 集成', group: 'knowledge', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/knowledge/rag-eval',
    name: 'RagEvaluation',
    component: () => import('@/views/knowledge/RagEvaluation.vue'),
    meta: { menuKey: 'knowledgeRagEval', menuSection: 'knowledge', menuParents: ["knowledgeWorkspace"], menuOrder: 8, menuIcon: 'CircleCheck', title: 'RAG 评测', group: 'knowledge', icon: 'DataLine', requiresAuth: true }
  }
]
