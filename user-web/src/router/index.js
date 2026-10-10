import { createRouter, createWebHashHistory } from 'vue-router'
import Layout from '@/layout/Layout.vue'
import { isInitialized } from '@/utils/initHelper'
import { useUserStore } from '@/stores/user'
import { ElMessage } from 'element-plus'

// ============================================================================
// 路由装配：全量、启动期一次性注册（模块元信息极轻，页面组件仍是懒加载）
// ----------------------------------------------------------------------------
// 旧实现是「按 URL 首段 + 手工白名单懒装配」：
//   moduleNames（模块名白名单）+ pathToModule（kebab 首段→camel 模块名映射）
//   + beforeEach 里的 ensureRouteLoaded 动态 addRoute。
// 它是「大量页面点进去报页面不存在」的根因——白名单要人工维护，任一环节漏登记：
//   · 新增模块忘了进 moduleNames           → 永不 addRoute
//   · 白名单里写了名字但没有文件           → lazyModule() 返 null（如 capMatrix）
//   · 首段 kebab-case 与模块名不一致且没映射 → 按首段找不到模块（tiktok-card-stats）
//   · 路由挂在别的模块文件里（/cap/matrix 在 kbConnector.js）
// 结果都是同一种用户可见故障：菜单/深链能点，但路由表里没有 ⇒ NotFound「页面不存在」。
//
// 现在改为 import.meta.glob(..., { eager: true }) 在启动期注册 modules/ 下**全部**模块：
//   · 路由文件存在 = 路由存在，不再需要任何白名单，漏登记这一类故障从机制上消失
//   · 侧边栏由 router.getRoutes() 自动生成（见 layout/menu.js），不再有第二份手写菜单树
//   · 页面组件依旧 () => import('@/views/...')，首屏体积不受影响
// ============================================================================
const routeModules = import.meta.glob('./modules/*.js', { eager: true })

const moduleRoutes = []
for (const [file, mod] of Object.entries(routeModules)) {
  const routes = mod && mod.default
  if (Array.isArray(routes)) {
    moduleRoutes.push(...routes)
  } else if (routes && typeof routes === 'object') {
    // 兼容导出单个路由对象（历史上 dingtalkApp / whatsappCloud 就是这种写法）
    moduleRoutes.push(routes)
  } else {
    console.error(`[router] 路由模块没有 default 导出：${file}`)
  }
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

const routes = [
  {
    path: '/',
    name: 'Layout',
    component: Layout,
    redirect: '/messageHub/list',
    children: [
      // modules/ 下的全部业务路由（绝对路径，直接挂在 Layout 下）
      ...moduleRoutes,
      {
        path: 'profile',
        name: 'Profile',
        component: () => import('@/views/Profile.vue'),
        meta: { title: '个人资料', requiresAuth: true, menuHidden: true }
      },
      {
        path: 'notifications',
        name: 'Notifications',
        component: () => import('@/views/Notifications.vue'),
        meta: { title: '通知中心', requiresAuth: true, menuHidden: true }
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

router.beforeEach((to) => {
  const isPublicPath = to.path === '/setup' || to.path === '/login' || to.path.startsWith('/chat/embed');
  const isPublic = to.meta?.public === true || isPublicPath

  if (!isPublic) {
    if (!isInitialized()) {
      return '/setup'
    }
    const userStore = useUserStore()
    if (!userStore.isLoggedIn) {
      return '/login'
    }
    if (to.meta?.requiresAdmin && userStore.role !== 'admin') {
      ElMessage.error('无权访问（403）：仅管理员可使用该功能')
      return { name: 'NotFound', query: { status: '403', from: to.fullPath }, replace: true }
    }
  }
  return true
})

export default router
