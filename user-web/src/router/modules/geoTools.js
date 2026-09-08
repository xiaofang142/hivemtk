export default [
  {
    path: '/geo-tools/visibility',
    name: 'GeoVisibilityBoard',
    component: () => import('@/views/geo/VisibilityBoard.vue'),
    meta: { title: '可见性观测', group: 'analytics', icon: 'TrendCharts', requiresAuth: true }
  },
  {
    path: '/geo/decision-report',
    name: 'GeoDecisionReport',
    component: () => import('@/views/geo/DecisionReport.vue'),
    meta: { title: '决策链报表', group: 'analytics', icon: 'DataAnalysis', requiresAuth: true }
  },
  {
    path: '/geo-tools/keyword-mining',
    name: 'GeoKeywordMining',
    component: () => import('@/views/geo/KeywordMining.vue'),
    meta: { title: '关键词蒸馏', group: 'analytics', icon: 'Search', requiresAuth: true }
  },
  {
    path: '/geo-tools/content-creation',
    name: 'GeoContentCreation',
    component: () => import('@/views/geo/ContentCreation.vue'),
    meta: { title: '内容创作', group: 'analytics', icon: 'EditPen', requiresAuth: true }
  },
  {
    path: '/geo-tools/content-optimize',
    name: 'GeoContentOptimize',
    component: () => import('@/views/geo/ContentOptimize.vue'),
    meta: { title: '文章优化', group: 'analytics', icon: 'Document', requiresAuth: true }
  },
  {
    path: '/geo-tools/knowledge-base',
    name: 'GeoKnowledgeBase',
    component: () => import('@/views/geo/KnowledgeBase.vue'),
    meta: { title: 'GEO 知识库', group: 'analytics', icon: 'FolderOpened', requiresAuth: true }
  },
  {
    path: '/geo-tools/platform-publish',
    name: 'GeoPlatformPublish',
    component: () => import('@/views/geo/PlatformPublish.vue'),
    meta: { title: '平台发布', group: 'analytics', icon: 'Connection', requiresAuth: true }
  },
  {
    path: '/geo-tools/workflow',
    name: 'GeoWorkflow',
    component: () => import('@/views/geo/WorkflowEditor.vue'),
    meta: { title: '工作流', group: 'analytics', icon: 'Setting', requiresAuth: true }
  },
  {
    path: '/geo-tools/sov-board',
    name: 'GeoSovBoard',
    component: () => import('@/views/geo/SovBoard.vue'),
    meta: { title: '竞品 SOV', group: 'analytics', icon: 'DataLine', requiresAuth: true }
  },
  {
    path: '/geo-tools/crawler-stats',
    name: 'GeoCrawlerStats',
    component: () => import('@/views/geo/CrawlerStats.vue'),
    meta: { title: '爬虫统计', group: 'analytics', icon: 'Monitor', requiresAuth: true }
  },
  {
    path: '/geo-tools/entity-graph',
    name: 'GeoEntityGraph',
    component: () => import('@/views/geo/EntityGraph.vue'),
    meta: { title: '实体图谱', group: 'analytics', icon: 'Share', requiresAuth: true }
  },
  {
    path: '/geo-tools/verification',
    name: 'GeoVerification',
    component: () => import('@/views/geo/Verification.vue'),
    meta: { title: '多模型验证', group: 'analytics', icon: 'CircleCheck', requiresAuth: true }
  },
  {
    path: '/geo-tools/reports',
    name: 'GeoReports',
    component: () => import('@/views/geo/Reports.vue'),
    meta: { title: '数据报表', group: 'analytics', icon: 'DataAnalysis', requiresAuth: true }
  },
  {
    path: '/geo-tools/config',
    name: 'GeoConfig',
    component: () => import('@/views/geo/ConfigOptimizer.vue'),
    meta: { title: '配置优化', group: 'analytics', icon: 'Setting', requiresAuth: true }
  },
  {
    path: '/geo-tools/competitors',
    name: 'GeoCompetitors',
    component: () => import('@/views/geo/CompetitorManage.vue'),
    meta: { title: '竞品管理', group: 'analytics', icon: 'Users', requiresAuth: true }
  },
  {
    path: '/geo-tools/alerts',
    name: 'GeoAlertCenter',
    component: () => import('@/views/geo/AlertCenter.vue'),
    meta: { title: '告警中心', group: 'analytics', icon: 'Bell', requiresAuth: true }
  }
]
