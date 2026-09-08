import { createRouter, createWebHashHistory } from 'vue-router'
import Layout from '@/layout/Layout.vue'
import { isInitialized } from '@/utils/initHelper'
import { useUserStore } from '@/stores/user'
import { ElMessage } from 'element-plus'

const routeModules = import.meta.glob('./modules/*.js');

const lazyModule = (name) => {
  const key = `./modules/${name}.js`
  return routeModules[key] || null
}

const initRoutes = [
  {
    path: '/setup',
    name: 'InitSetup',
    component: () => import('@/views/setup/InitSetup.vue'),
    meta: { title: '系统初始化' }
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/Login.vue'),
    meta: { title: '登录' }
  },
  {
    path: '/help-center',
    name: 'HelpCenter',
    component: () => import('@/views/public/HelpCenter.vue'),
    meta: { title: '帮助中心', requiresAuth: false, public: true, hideLayout: true }
  },
  {
    path: '/chat/embed/default',
    name: 'ChatEmbed',
    component: () => import('@/views/chat/embed/Index.vue'),
    meta: { title: '在线客服', requiresAuth: false, public: true, hideLayout: true }
  },
  {
    path: '/chat/embed/:channel_ref',
    name: 'ChatEmbedChannel',
    component: () => import('@/views/chat/embed/Index.vue'),
    meta: { title: '在线客服', requiresAuth: false, public: true, hideLayout: true }
  },
  {
    path: '/chat/embed',
    redirect: '/chat/embed/default'
  },
  {
    path: '/chat/embed/',
    redirect: '/chat/embed/default'
  }
];

const moduleNames = [
  'email', 'telegram', 'qq', 'whatsapp', 'clue', 'system',
  'domainPool', 'shortLink', 'douyinCard', 'xiaohongshuCard', 'kuaishouCard',
  'xianyuCard', 'sms', 'livecode', 'tiktok',
  'abExperiment', 'batchOperation', 'churnPrediction',
  'customReport', 'customer360', 'customerEvent', 'customerSession',
  "oneid",
  'dashboardScreen', 'integration', 'marketingFlow', 'operationLog',
  'ragProductConfig', 'scriptTemplate', 'userSegment',
  'community', 'unifiedMessage', 'platformAccount', 'messageHub',
  "intentRecognition", 'dialogueMemory', 'sopAgent',
  "reachPipeline", 'wecomAccount',
  'whatsappCloud',
  'dingtalkApp',
  "llmRouting", 'tagSegmentation', 'conversionFunnel',
  'aiProductivity',
  "knowledge",
  "aiAgent",
  "faq", 'sopTemplateKb',
  "knowledgeBase",
  "assetMarket",
  "assetBundle",
  "customerService",
  "chatChannel",
  "objection", 'persona',
  "customerJourney",
  "backup", 'securityAudit',
  "workflowOrchestrator",
  "feishu",
  "tuning",
  "systemUser",
  "role",
  "permission",
  "glossary", 'i18nStats',
  "crossPublish",
  "ops",
  "geoTools",
  "analytics",
  "bridgeToken",
  "leadMining",
];

const eagerLoadedRoutes = [];
const lazyRoutes = [];

const routes = [
  {
    path: '/',
    name: 'Layout',
    component: Layout,
    redirect: '/messageHub/list',
    children: [
      ...eagerLoadedRoutes,
      {
        path: 'profile',
        name: 'Profile',
        component: () => import('@/views/Profile.vue'),
        meta: { title: '个人资料', requiresAuth: true }
      },
      {
        path: 'notifications',
        name: 'Notifications',
        component: () => import('@/views/Notifications.vue'),
        meta: { title: '通知中心', requiresAuth: true }
      }
    ],
  },
  ...initRoutes,
  {
    path: '/telegram/group',
    redirect: '/telegram/account'
  },
  {
    path: '/oneid',
    redirect: '/oneid/list'
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/NotFound.vue'),
    meta: { title: '页面不存在' }
  }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

const loadedModules = new Set();
const loadedRoutes = []

const pathToModule = {
  'douyin': 'douyinCard',
  'xiaohongshu': 'xiaohongshuCard',
  'kuaishou': 'kuaishouCard',
  'xianyu': 'xianyuCard',
  'douyin-card-stats': 'douyinCard',
  'xiaohongshu-card-stats': 'xiaohongshuCard',
  'kuaishou-card-stats': 'kuaishouCard',
  'xianyu-card-stats': 'xianyuCard',
  'asset-market': 'assetMarket',
  'asset-bundle': 'assetBundle',
  'confidence': 'tuning',
  'humanize': 'tuning',
  'feedbackLoop': 'tuning',
  'whatsapp-cloud': 'whatsappCloud',
  'dingtalk-app': 'dingtalkApp',
  'i18n': 'i18nStats',
  'sop-template': 'sopTemplateKb',
  'ops-overview': 'ops',
  'sales-cockpit': 'ops',
  'cards': 'crossPublish',
  'workflow-orchestrator': 'workflowOrchestrator',
  'geo-tools': 'geoTools',
  'geo': 'geoTools',
  'bridge': 'bridgeToken',
  'token': 'bridgeToken',
  'lead-mining': 'leadMining',
};

async function ensureRouteLoaded(path) {
  const pathSegments = path.split('/').filter(Boolean);
  if (pathSegments.length === 0) return false

  const moduleName = pathToModule[pathSegments[0]] || pathSegments[0]
  if (!moduleNames.includes(moduleName)) return false
  if (loadedModules.has(moduleName)) return false

  try {
    const lazyFn = lazyModule(moduleName);
    if (!lazyFn) return false
    const mod = await lazyFn()
    if (!mod || !mod.default) {
      console.warn(`[router] 模块 ${moduleName} 加载失败或无 default 导出`)
      return false
    }
    const routes = Array.isArray(mod.default) ? mod.default : [mod.default]
    for (const r of routes) {
      try { router.addRoute('Layout', r) } catch (e) {}
    }
    loadedModules.add(moduleName)
    return true
  } catch (e) {
    console.error(`[router] 模块 ${moduleName} 加载异常`, e)
    return false
  }
}

router.beforeEach(async (to, from, next) => {
  const routeLoaded = await ensureRouteLoaded(to.path);

  if (to.path.startsWith('/system/')) {
    const extraModules = ['ragProductConfig', 'systemUser', 'role', 'permission']
    for (const modName of extraModules) {
      if (loadedModules.has(modName)) continue
      try {
        const mod = await lazyModule(modName)()
        const moduleRoutes = mod.default || mod
        if (Array.isArray(moduleRoutes)) {
          moduleRoutes.forEach(route => {
            router.addRoute('Layout', route)
            loadedRoutes.push(route)
          })
        }
        loadedModules.add(modName)
      } catch (err) {
        console.error(`[Lazy Router] Failed to load ${modName}:`, err)
      }
    }
  }

  const isPublicPath = to.path === '/setup' || to.path === '/login' || to.path.startsWith('/chat/embed');
  const isPublic = to.meta?.public === true || isPublicPath

  if (!isPublic) {
    if (!isInitialized()) {
      next('/setup')
      return
    }
    const userStore = useUserStore()
    if (!userStore.isLoggedIn) {
      next('/login')
      return
    }
    if (to.meta?.requiresAdmin && userStore.role !== 'admin') {
      ElMessage.error('无权访问（403）：仅管理员可使用该功能')
      next({ name: 'NotFound', query: { status: '403', from: to.fullPath }, replace: true })
      return
    }
  }

  if (routeLoaded) {
    next({ path: to.path, replace: true })
  } else {
    if (to.matched.length === 0) {
      next({ name: 'NotFound', replace: true })
    } else {
      next()
    }
  }
})
export default router
