export default [
  {
    path: '/system/config',
    name: 'SystemConfig',
    component: () => import('@/views/system/Config.vue'),
    meta: { title: '站点设置', group: 'system', icon: 'Tools', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/obs-config',
    name: 'SystemObsConfig',
    component: () => import('@/views/system/ObsConfig.vue'),
    meta: { title: '存储配置', group: 'system', icon: 'Cloud', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/material-library',
    name: 'SystemMaterialLibrary',
    component: () => import('@/views/system/MaterialLibrary.vue'),
    meta: { title: '素材库', group: 'system', icon: 'Picture', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/monitor',
    name: 'SystemMonitor',
    component: () => import('@/views/system/Monitor.vue'),
    meta: { title: '监控', group: 'system', icon: 'Cpu', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/trace',
    name: 'TraceMonitor',
    component: () => import('@/views/system/TraceMonitor.vue'),
    meta: { title: '链路追踪', group: 'system', icon: 'Connection', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/guide',
    name: 'SystemGuide',
    component: () => import('@/views/system/Guide.vue'),
    meta: { title: '使用引导', group: 'system', icon: 'Document', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/rag-overview',
    name: 'RagOverview',
    component: () => import('@/views/system/RagOverview.vue'),
    meta: { title: 'RAG概览', group: 'knowledge', icon: 'Monitor', requiresAuth: true }
  },
  {
    path: '/system/automation-hub',
    name: 'AutomationHub',
    component: () => import('@/views/system/AutomationHub.vue'),
    meta: { title: '自动化中心', group: 'system', icon: 'Setting', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/config-params',
    name: 'ConfigParams',
    component: () => import('@/views/system/ConfigParams.vue'),
    meta: { title: '动态阈值参数', group: 'system', icon: 'DataAnalysis', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/ltc-config',
    name: 'LTCConfig',
    component: () => import('@/views/system/LTCConfig.vue'),
    // requiresAdmin 与后端一致：这两个端点挂在 AdminAuthMiddleware 之后。
    // /system/config-params 那族参数今天只要求登录（遗留口径），本卡不跟它。
    meta: { title: 'LTC 运营开关', group: 'system', icon: 'Open', requiresAuth: true, requiresAdmin: true }
  }
]
