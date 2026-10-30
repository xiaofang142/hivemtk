import { createRouter, createWebHashHistory } from 'vue-router'
import Layout from '@/layout/Layout.vue'
import { ensureBootstrap } from '@/utils/bootstrapState'
import { useUserStore } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'
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
  'inbox',
  'approvalTask',
  'badCase',
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
  "ops",
  "geoTools",
  "analytics",
  "bridgeToken",
  "leadMining",
  "browserAutomation",
  "orderDraft",
  "salesWorkbench",
  'followups',
  "unknownQuestion",
  "llmReport",
  "kbConnector",
  "mcpCredential",
  "observabilityDashboard",
  "templateMarket",
  // 功能模块市场 + 商户注册（模块化改造）
  "moduleMarket",
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
    // public: 404 页是静态页、无租户数据，且是若干**公开链路**的终点：
    // 未登录访客撞到不存在的地址时，应该看到「页面不存在」而不是被守卫推进 /login
    // （嵌在第三方站点的 iframe 里更突兀）。公开标记只影响「未登录时是否放行」，
    // 不泄露任何业务数据。客服挂件「关闭」自 2026-10 起改为页内展示「会话已结束」，
    // 不再跳本页。
    meta: { title: '页面不存在', public: true }
  }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

const loadedModules = new Set();
const loadedRoutes = []

const pathToModule = {
  // 订单草稿页挂在 /dashboard/drafts（与工作台聚合待办生成的深链对齐），
  // 首段 'dashboard' 映射到 orderDraft 模块 —— ensureRouteLoaded 按首段找模块名，
  // 没有这条映射，深链会落进 NotFound 而不是草稿页。
  'dashboard': 'orderDraft',
  // 销售工作台页挂在 /sales-workbench，首段 'sales-workbench'（含连字符）
  // 映射到 salesWorkbench 模块 —— 不映射则按原样找模块名找不到，进不了懒装配白名单。
  'sales-workbench': 'salesWorkbench',
  'douyin': 'douyinCard',
  'xiaohongshu': 'xiaohongshuCard',
  'kuaishou': 'kuaishouCard',
  'xianyu': 'xianyuCard',
  'douyin-card-stats': 'douyinCard',
  'xiaohongshu-card-stats': 'xiaohongshuCard',
  'kuaishou-card-stats': 'kuaishouCard',
  'xianyu-card-stats': 'xianyuCard',
  // 缺这一条时，/#/tiktok-card-stats/20 只有"先从列表页点进去"才装得上模块；
  // 刷新或新开深链会按首段找模块名，找不到就落进 404 页。
  'tiktok-card-stats': 'tiktok',
  'asset-market': 'assetMarket',
  'asset-bundle': 'assetBundle',
  // T1 未知问题运营页挂在 /unknown-questions（含连字符）
  'unknown-questions': 'unknownQuestion',
  // T3 LLM 观测报表挂在 /reports/llm（首段 reports）
  'reports': 'llmReport',
  // 功能模块市场（模块化改造）
  'module-market': 'moduleMarket',
  'register': 'moduleMarket',
  // 观测看板（007 D2/B2）与模板市场（013 F2）：路由路径含连字符，首段映射到
  // 驼峰模块名，否则深链/刷新按首段找不到模块、落 404（与 asset-market 同族）。
  'observability-dashboard': 'observabilityDashboard',
  'template-market': 'templateMarket',
  // 续费墙（003 T023）：4305 跳转目标，路由注册在 moduleMarket 模块内
  'subscription-wall': 'moduleMarket',
  'confidence': 'tuning',
  'humanize': 'tuning',
  'feedbackLoop': 'tuning',
  'whatsapp-cloud': 'whatsappCloud',
  'dingtalk-app': 'dingtalkApp',
  'i18n': 'i18nStats',
  'sop-template': 'sopTemplateKb',
  'ops-overview': 'ops',
  'sales-cockpit': 'ops',
  'workflow-orchestrator': 'workflowOrchestrator',
  'geo-tools': 'geoTools',
  'geo': 'geoTools',
  'bridge': 'bridgeToken',
  'token': 'bridgeToken',
  'lead-mining': 'leadMining',
  'browser-automation': 'browserAutomation',
  'kb': 'kbConnector',
  'mcp': 'mcpCredential',
  'cap': 'kbConnector',
  'cron': 'browserAutomation',
  'sessions': 'browserAutomation',
  'status': 'browserAutomation',
  'tasks': 'browserAutomation',
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
      try { router.addRoute('Layout', r) } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
    }
    loadedModules.add(moduleName)
    return true
  } catch (e) {
    console.error(`[router] 模块 ${moduleName} 加载异常`, e)
    return false
  }
}

router.beforeEach(async (to, from, next) => {
  // guard 开始时的登录态快照：await 期间 401 处理可能清掉 token，
  // 两道 401 竞态闸靠「曾有 token 而现在没了」识别这种会话失效（见下）。
  const hadTokenAtGuardStart = !!localStorage.getItem('token')
  // 登录态单一真相是 localStorage（bug: web-token-expired-no-redirect）：
  // request.js 的 401 处理只清 localStorage，而 Pinia 的 token ref 只在
  // initAuth 读过一次 —— 同文档内的 401 之后 store 里是僵尸 isLoggedIn，
  // 会把已失效会话放进受保护页面（表现为"提示过期但停在原页"）。
  // 守卫每次先对齐两者：localStorage 为空时把残留 ref 一并清掉。
  const userStoreEarly = useUserStore()
  if (!hadTokenAtGuardStart && userStoreEarly.isLoggedIn) {
    userStoreEarly.setToken('')
  }
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

  // /register 必须在白名单里：它是懒加载路由（moduleMarket 模块），首次导航时
  // 下面 ensureRouteLoaded 刚 addRoute 的 meta 还没解析进 to.meta，不兜底会被
  // 误判成非公开路由，干净浏览器点「注册商户账号」直接被推进 /setup 或 /login。
  const isPublicPath = to.path === '/setup' || to.path === '/login' || to.path === '/register' || to.path.startsWith('/chat/embed');
  // 不能只信 to.meta：vue-router 4 的 to 在本次导航开始时已解析完毕，ensureRouteLoaded
  // 动态 addRoute 的 meta（如 /register 的 public:true）不会回填到这个 to 上。
  // addRoute 之后从 router.getRoutes() 重读真实注册的 meta 再判一次。
  let isPublic = to.meta?.public === true || isPublicPath
  if (!isPublic) {
    const registered = router.getRoutes().find((r) => r.path === to.path)
    if (registered?.meta?.public === true) isPublic = true
  }

  if (!isPublic) {
    // 首次进入非公开路由时，先等引导状态拉齐（只等这一次，之后走缓存 promise，
    // 不会逐次网络往返）：initialized 以后端 init-status 为准，而非 localStorage ——
    // 干净浏览器首次访问时 localStorage 为空，仅凭它会把所有人误推进 /setup。
    const { initialized } = await ensureBootstrap()
    if (!initialized) {
      next('/setup')
      return
    }    const userStore = useUserStore()
    // 401 竞态闸（bug: web-token-expired-no-redirect）：guard 开始时持有 token、
    // 走到这里已被清掉的，说明 request.js 已在 await 期间 push('/login')；
    // 此时必须由本 guard 亲自去 /login 并返回，否则下方任何一个 next() 决策都会
    // 把被中断的原导航收尾回原页面，覆盖 /login（表现为"提示过期但不跳登录"）。
    // 判据直读 localStorage（401 清空的正是它），不用 Pinia —— store 的 token ref
    // 只在 initAuth 时读一次 localStorage，401 后是僵尸值，靠不住。
    if (hadTokenAtGuardStart && !localStorage.getItem('token')) {
      next('/login')
      return
    }
    // 系统已初始化：直接去登录页，绝不把“已初始化实例”的干净浏览器访客单机
    // 送进 /setup 安装向导。SaaS(tenant_mode=on) 多租户实例尤其如此——那里没有
    // “单机首次安装”场景，注册入口在登录页（Login.vue 的 enable_register 开关）。
    if (!userStore.isLoggedIn) {
      next('/login')
      return
    }
    if (to.meta?.requiresAdmin && userStore.role !== 'admin') {
      ElMessage.error('无权访问（403）：仅管理员可使用该功能')
      next({ name: 'NotFound', query: { status: '403', from: to.fullPath }, replace: true })
      return
    }
    // RBAC 菜单权限：登录后拉取后端菜单树（缓存），非超管校验路径授权
    const permissionStore = usePermissionStore()
    await permissionStore.ensureMenus()
    // 第二道 401 竞态闸：ensureMenus 的请求正是不带 _silent 的业务请求，
    // 过期 token 的 401 清会话就发生在上面的 await 期间——必须复查后再做决策。
    if (hadTokenAtGuardStart && !localStorage.getItem('token')) {
      next('/login')
      return
    }
    if (userStore.role !== 'admin') {
      const personalPaths = ['/profile', '/notifications']
      const isPersonal = personalPaths.some(p => to.path === p || to.path.startsWith(p + '/'))
      if (!isPersonal && !permissionStore.canAccessPath(to.path)) {
        // 默认落地页不可访问时重定向到首个授权菜单，避免登录后直接 403
        if (to.path === '/messageHub/list') {
          next({ path: permissionStore.menuPaths[0] || '/inbox/list', replace: true })
          return
        }
        ElMessage.error('无权访问（403）：请联系管理员分配菜单权限')
        next({ name: 'NotFound', query: { status: '403', from: to.fullPath }, replace: true })
        return
      }
    }
  }

  // NotFound（404 兜底路由）现在是公开路由（public: true，见上面的 meta 注释），
  // 于是它绕过了上面 if (!isPublic) 里的引导状态判断 —— 后果是「尚未安装」
  // 的实例上，访客敲一个不存在的地址会看到 404 页而不是安装向导。
  // 装好之前（initialized=false）仍然推进 /setup，与改动前的行为一致；
  // /login、/register、/chat/embed 这些真正公开的页面不受影响。
  if (isPublic && to.name === 'NotFound') {
    const { initialized } = await ensureBootstrap()
    if (!initialized) {
      next('/setup')
      return
    }
  }

  if (routeLoaded) {
    // 必须带上 query 与 hash：这一支是"模块刚装好，重新走一次导航"，只给 path 等于把
    // 深链参数抹了。实测（真实例 + 真浏览器）新开页 http://…/#/customerSession/list?session_id=X
    // 落地后地址栏是 #/customerSession/list（参数没了），于是 CSAT 看板「查看会话」
    // 永远选不中那条会话——页面里再怎么写都白写。同一个会话里第二次进这一页才保留参数，
    // 因为那时 routeLoaded 已经是 false，不会重定向。
    next({ path: to.path, query: to.query, hash: to.hash, replace: true })
  } else {
    if (to.matched.length === 0) {
      next({ name: 'NotFound', replace: true })
    } else {
      next()
    }
  }
})
export default router
