const loadList = () => import('@/views/KnowledgeWorkspace/KnowledgeManagement.vue')
const listPath = '/knowledge/management'

export default [
  {
    path: '/knowledge/management',
    name: 'KnowledgeManagement',
    component: loadList,
    meta: { title: '知识库管理', group: 'knowledge', icon: 'Files', requiresAuth: true }
  },
  {
    path: '/knowledge/batch-import',
    name: 'KnowledgeBatchImport',
    components: { default: () => import('@/views/KnowledgeWorkspace/BatchImport.vue'), list: loadList },
    meta: { title: '批量导入', group: 'knowledge', icon: 'UploadFilled', requiresAuth: true, listEditor: listPath }
  },
  {
    path: '/knowledge/playground',
    name: 'KnowledgePlayground',
    component: () => import('@/views/KnowledgeWorkspace/Playground.vue'),
    meta: { title: '检索 Playground', group: 'knowledge', icon: 'Aim', requiresAuth: true }
  },
  {
    path: '/knowledge/connectors',
    name: 'KnowledgeConnectors',
    component: () => import('@/views/KnowledgeWorkspace/Connectors.vue'),
    meta: { title: '外部连接器', group: 'knowledge', icon: 'Link', requiresAuth: true }
  },
  {
    path: '/knowledge/crawler-sources',
    name: 'KnowledgeCrawlerSources',
    component: () => import('@/views/KnowledgeWorkspace/CrawlerSources.vue'),
    meta: { title: '网站爬取', group: 'knowledge', icon: 'Download', requiresAuth: true }
  },
  {
    path: '/knowledge/qa-candidates',
    name: 'KnowledgeQACandidates',
    component: () => import('@/views/KnowledgeWorkspace/QACandidates.vue'),
    meta: { title: 'QA 审核队列', group: 'knowledge', icon: 'Finished', requiresAuth: true }
  },
  {
    path: '/knowledge/feedbacks',
    name: 'KnowledgeFeedbacks',
    component: () => import('@/views/KnowledgeWorkspace/FeedbackList.vue'),
    meta: { title: '反馈管理', group: 'knowledge', icon: 'ChatLineRound', requiresAuth: true }
  },
  {
    path: '/knowledge/tokens',
    name: 'KnowledgeTokens',
    component: () => import('@/views/KnowledgeWorkspace/ApiToken.vue'),
    meta: { title: 'API Token', group: 'knowledge', icon: 'Key', requiresAuth: true }
  },
  {
    path: '/knowledge/external',
    name: 'KnowledgeExternal',
    component: () => import('@/views/KnowledgeWorkspace/ExternalImport.vue'),
    meta: { title: '外部系统接入', group: 'knowledge', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/knowledge/statistics',
    name: 'KnowledgeStatistics',
    component: () => import('@/views/KnowledgeWorkspace/KnowledgeStatistics.vue'),
    meta: { title: '知识库统计', group: 'knowledge', icon: 'DataAnalysis', requiresAuth: true }
  },
  {
    path: '/knowledge/openapi',
    name: 'KnowledgeOpenAPI',
    component: () => import('@/views/KnowledgeWorkspace/OpenAPIIntegration.vue'),
    meta: { title: 'OpenAPI 集成', group: 'knowledge', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/knowledge/rag-eval',
    name: 'RagEvaluation',
    component: () => import('@/views/knowledge/RagEvaluation.vue'),
    meta: { title: 'RAG 评测', group: 'knowledge', icon: 'DataLine', requiresAuth: true }
  }
]
