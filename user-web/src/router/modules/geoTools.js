// ═══════════════════════════════════════════════════════
// GEO-TOOLS 路由重构：Config → Execute → Observe
// 整合 GEO v2 全链路（关键词→文章→部署→推送→收录验证）
// ═══════════════════════════════════════════════════════

export default [
  // ────────────────────────────────────────────────────
  // Layer 1: 配置 (group: 'config')
  // ────────────────────────────────────────────────────
  {
    path: '/geo-tools/brand-config',
    name: 'GeoBrandConfig',
    component: () => import('@/views/geo/BrandConfig.vue'),
    meta: { title: '品牌与域名', group: 'config', icon: 'OfficeBuilding', order: 1, requiresAuth: true }
  },
  {
    path: '/geo-tools/seo-infra',
    name: 'GeoSeoInfra',
    component: () => import('@/views/geo/SeoInfra.vue'),
    meta: { title: 'SEO 基础设施', group: 'config', icon: 'DocumentChecked', order: 2, requiresAuth: true }
  },
  {
    path: '/geo-tools/pusher-config',
    name: 'GeoPusherConfig',
    component: () => import('@/views/geo/PusherConfig.vue'),
    meta: { title: '蜘蛛推送配置', group: 'config', icon: 'Key', order: 3, requiresAuth: true }
  },
  {
    path: '/geo-tools/site-config',
    name: 'GeoSiteConfig',
    component: () => import('@/views/geo/SiteConfig.vue'),
    meta: { title: '静态站部署', group: 'config', icon: 'Monitor', order: 4, requiresAuth: true }
  },
  {
    path: '/geo-tools/schema-templates',
    name: 'GeoSchemaTemplates',
    component: () => import('@/views/geo/SchemaTemplates.vue'),
    meta: { title: 'Schema 模板', group: 'config', icon: 'Collection', order: 5, requiresAuth: true }
  },
  {
    path: '/geo-tools/competitors',
    name: 'GeoCompetitors',
    component: () => import('@/views/geo/CompetitorManage.vue'),
    meta: { title: '竞品管理', group: 'config', icon: 'Users', order: 6, requiresAuth: true }
  },

  // ────────────────────────────────────────────────────
  // Layer 2: 执行 (group: 'execute')
  // ────────────────────────────────────────────────────
  {
    path: '/geo-tools/keyword-mining',
    name: 'GeoKeywordMining',
    component: () => import('@/views/geo/KeywordMining.vue'),
    meta: { title: '关键词蒸馏', group: 'execute', icon: 'Search', order: 1, requiresAuth: true }
  },
  {
    path: '/geo-tools/content-creation',
    name: 'GeoContentCreation',
    component: () => import('@/views/geo/ContentCreation.vue'),
    meta: { title: '内容创作', group: 'execute', icon: 'EditPen', order: 2, requiresAuth: true }
  },
  {
    path: '/geo-tools/content-optimize',
    name: 'GeoContentOptimize',
    component: () => import('@/views/geo/ContentOptimize.vue'),
    meta: { title: '文章优化', group: 'execute', icon: 'Document', order: 3, requiresAuth: true }
  },
  {
    path: '/geo-tools/site-publish',
    name: 'GeoSitePublish',
    component: () => import('@/views/geo/SitePublish.vue'),
    meta: { title: '发布到官网', group: 'execute', icon: 'Upload', order: 4, requiresAuth: true }
  },
  {
    path: '/geo-tools/push-center',
    name: 'GeoPushCenter',
    component: () => import('@/views/geo/PushCenter.vue'),
    meta: { title: '蜘蛛推送', group: 'execute', icon: 'Promotion', order: 5, requiresAuth: true }
  },
  {
    path: '/geo-tools/platform-publish',
    name: 'GeoPlatformPublish',
    component: () => import('@/views/geo/PlatformPublish.vue'),
    meta: { title: '多平台发布', group: 'execute', icon: 'Connection', order: 6, requiresAuth: true }
  },
  {
    path: '/geo-tools/workflow',
    name: 'GeoWorkflow',
    component: () => import('@/views/geo/WorkflowEditor.vue'),
    meta: { title: '工作流', group: 'execute', icon: 'Setting', order: 7, requiresAuth: true }
  },
  {
    path: '/geo-tools/knowledge-base',
    name: 'GeoKnowledgeBase',
    component: () => import('@/views/geo/KnowledgeBase.vue'),
    meta: { title: 'GEO 知识库', group: 'execute', icon: 'FolderOpened', order: 8, requiresAuth: true }
  },
  {
    path: '/geo-tools/entity-graph',
    name: 'GeoEntityGraph',
    component: () => import('@/views/geo/EntityGraph.vue'),
    meta: { title: '实体图谱', group: 'execute', icon: 'Share', order: 9, requiresAuth: true }
  },

  // ────────────────────────────────────────────────────
  // Layer 3: 监控 (group: 'observe')
  // ────────────────────────────────────────────────────
  {
    path: '/geo-tools/funnel-dashboard',
    name: 'GeoFunnelDashboard',
    component: () => import('@/views/geo/FunnelDashboard.vue'),
    meta: { title: '漏斗总览', group: 'observe', icon: 'DataAnalysis', order: 1, requiresAuth: true }
  },
  {
    path: '/geo-tools/index-tracking',
    name: 'GeoIndexTracking',
    component: () => import('@/views/geo/IndexTracking.vue'),
    meta: { title: '收录与引用', group: 'observe', icon: 'CircleCheck', order: 2, requiresAuth: true }
  },
  {
    path: '/geo-tools/visibility',
    name: 'GeoVisibilityBoard',
    component: () => import('@/views/geo/VisibilityBoard.vue'),
    meta: { title: '可见性趋势', group: 'observe', icon: 'TrendCharts', order: 3, requiresAuth: true }
  },
  {
    path: '/geo-tools/sov-board',
    name: 'GeoSovBoard',
    component: () => import('@/views/geo/SovBoard.vue'),
    meta: { title: '竞品 SOV', group: 'observe', icon: 'DataLine', order: 4, requiresAuth: true }
  },
  {
    path: '/geo-tools/crawler-stats',
    name: 'GeoCrawlerStats',
    component: () => import('@/views/geo/CrawlerStats.vue'),
    meta: { title: '爬虫统计', group: 'observe', icon: 'Monitor', order: 5, requiresAuth: true }
  },
  {
    path: '/geo-tools/verification',
    name: 'GeoVerification',
    component: () => import('@/views/geo/Verification.vue'),
    meta: { title: '多模型验证', group: 'observe', icon: 'Check', order: 6, requiresAuth: true }
  },
  {
    path: '/geo/decision-report',
    name: 'GeoDecisionReport',
    component: () => import('@/views/geo/DecisionReport.vue'),
    meta: { title: '决策链报表', group: 'observe', icon: 'Histogram', order: 7, requiresAuth: true }
  },
  {
    path: '/geo-tools/reports',
    name: 'GeoReports',
    component: () => import('@/views/geo/Reports.vue'),
    meta: { title: '成本报表', group: 'observe', icon: 'Money', order: 8, requiresAuth: true }
  },
  {
    path: '/geo-tools/alerts',
    name: 'GeoAlertCenter',
    component: () => import('@/views/geo/AlertCenter.vue'),
    meta: { title: '告警中心', group: 'observe', icon: 'Bell', order: 9, requiresAuth: true }
  }
]
