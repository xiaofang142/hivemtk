export default [
  {
    path: '/system/config',
    name: 'SystemConfig',
    component: () => import('@/views/system/Config.vue'),
    meta: { menuKey: 'systemConfig', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 1, menuIcon: 'Tools', title: '站点设置', group: 'system', icon: 'Tools', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/obs-config',
    name: 'SystemObsConfig',
    component: () => import('@/views/system/ObsConfig.vue'),
    meta: { menuKey: 'systemObsConfig', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 2, menuIcon: 'Cloud', title: '存储配置', group: 'system', icon: 'Cloud', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/material-library',
    name: 'SystemMaterialLibrary',
    component: () => import('@/views/system/MaterialLibrary.vue'),
    meta: { menuKey: 'systemMaterialLibrary', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 3, menuIcon: 'Picture', title: '素材库', group: 'system', icon: 'Picture', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/monitor',
    name: 'SystemMonitor',
    component: () => import('@/views/system/Monitor.vue'),
    meta: { menuKey: 'systemMonitor', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 4, menuIcon: 'Cpu', title: '监控', group: 'system', icon: 'Cpu', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/trace',
    name: 'TraceMonitor',
    component: () => import('@/views/system/TraceMonitor.vue'),
    meta: { menuKey: 'systemTrace', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 5, menuIcon: 'Connection', title: '链路追踪', group: 'system', icon: 'Connection', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/guide',
    name: 'SystemGuide',
    component: () => import('@/views/system/Guide.vue'),
    meta: { menuKey: 'systemGuide', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 6, menuIcon: 'Document', title: '使用引导', group: 'system', icon: 'Document', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/rag-overview',
    name: 'RagOverview',
    component: () => import('@/views/system/RagOverview.vue'),
    meta: { menuKey: 'ragOverview', menuSection: 'knowledge', menuOrder: 3, menuIcon: 'Monitor', title: 'RAG概览', group: 'knowledge', icon: 'Monitor', requiresAuth: true }
  },
  {
    path: '/system/automation-hub',
    name: 'AutomationHub',
    component: () => import('@/views/system/AutomationHub.vue'),
    meta: { menuKey: 'automationCenter', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 10, menuRoles: ["admin"], menuIcon: 'Operation', title: '自动化中心', group: 'system', icon: 'Setting', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/config-params',
    name: 'ConfigParams',
    component: () => import('@/views/system/ConfigParams.vue'),
    meta: { menuKey: 'dynamicThreshold', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 11, menuRoles: ["admin"], menuIcon: 'DataLine', title: '动态阈值参数', group: 'system', icon: 'DataAnalysis', requiresAuth: true, requiresAdmin: true }
  },
  {
    path: '/system/ltc-config',
    name: 'LTCConfig',
    component: () => import('@/views/system/LTCConfig.vue'),
    // requiresAdmin 与后端一致：这两个端点挂在 AdminAuthMiddleware 之后。
    // /system/config-params 那族参数今天只要求登录（遗留口径），本卡不跟它。
    meta: { menuKey: 'ltcConfig', menuSection: 'system', menuParents: ["siteConfig"], menuOrder: 12, menuRoles: ["admin"], menuIcon: 'Open', title: 'LTC 运营开关', group: 'system', icon: 'Open', requiresAuth: true, requiresAdmin: true }
  }
]
