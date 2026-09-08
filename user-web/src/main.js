import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import pinia from './stores'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import './styles/index.scss'
import { updateRequestConfig, http } from './utils/request'
import i18n from './i18n'
import { applyDirection } from './i18n/locale'

// Element Plus 图标改为按需自动导入：
//   - 由 vite.config.js 中的 unplugin-vue-components + ElementPlusResolver 在编译期
//     扫描模板里使用的 <Edit />、<Search /> 等图标组件,自动注入 import 语句。
//   - 不再在运行期循环注册全量图标,减少首屏 bundle 体积。

applyDirection(i18n.global.locale.value)

const app = createApp(App)

// 全局错误兜底：Vue 渲染/生命周期异常与未捕获 Promise 拒绝统一落 console，
// 防止单点异常静默吞掉导致"页面白屏但无任何报错"的不可观测故障。
app.config.errorHandler = (err, instance, info) => {
  console.error('[Vue errorHandler]', info, err)
}
window.addEventListener('unhandledrejection', (event) => {
  console.error('[unhandledrejection]', event.reason)
})

app.use(router)
app.use(pinia)
app.use(ElementPlus)
app.use(i18n)

// 初始化API配置
updateRequestConfig().catch(error => {
  console.error('初始化API配置失败:', error)
})

// OPT-SEC-07：根据当前路由动态调整 CSP frame-ancestors
// /chat/embed 路由需要被第三方网站嵌入（embed-sdk 场景）
// 其他路由必须 frame-ancestors 'none' 防止点击劫持
router.afterEach((to) => {
  const cspMeta = document.querySelector('meta[http-equiv="Content-Security-Policy"]')
  if (!cspMeta) return

  if (to.path.startsWith('/chat/embed')) {
    // 允许被任意 origin 嵌入（embed-sdk 业务场景）
    // 限制:仅 /chat/embed 路由开放,其他路由保持 'none'
    const csp = cspMeta.getAttribute('content')
    const newCsp = csp.replace(/frame-ancestors\s+[^;]+/, "frame-ancestors *")
    cspMeta.setAttribute('content', newCsp)
  } else {
    // 默认严格模式:禁止嵌入
    const csp = cspMeta.getAttribute('content')
    if (csp && !csp.includes("frame-ancestors 'none'")) {
      const newCsp = csp.replace(/frame-ancestors\s+[^;]+/, "frame-ancestors 'none'")
      cspMeta.setAttribute('content', newCsp)
    }
  }
})

app.mount('#app')

// ============================================================================
// Service Worker 版本管理 — 彻底防旧页/404
// ----------------------------------------------------------------------------
// 为什么需要这段:
//   1. vite-plugin-pwa 改了 injectRegister: false — 关闭自动注入
//   2. 这里手动注册 SW, 加版本检测逻辑
//
// 执行时机: app.mount 之后 (不阻塞首屏)
// 执行步骤:
//   [1] fetch('/version.json') — 用 cache:'no-store' 绕过 HTTP/SW 缓存
//   [2] 对比 localStorage 里的 buildId
//   [3] buildId 变化 → 旧版 SW 存在 → unregister + 清 caches → reload
//   [4] buildId 不变 → 注册/更新 SW, 监听 updatefound → skipWaiting
// ============================================================================

const SW_VERSION_KEY = 'hivemtk-build-id'
const SW_REGISTERED_KEY = 'hivemtk-sw-registered'
const SW_RELOAD_COUNT_KEY = 'hivemtk-sw-reload-count'

async function manageServiceWorker() {
  // SW 不可用 (HTTP、隐私模式、老浏览器) — 跳过, 不影响功能
  if (!('serviceWorker' in navigator)) {
    console.info('[SW] serviceWorker 不可用, 跳过注册')
    return
  }

  try {
    // [1] 拉取服务器上的 version.json (强制绕过缓存)
    const resp = await fetch('/version.json', { cache: 'no-store' })
    if (!resp.ok) {
      console.warn(`[SW] 拉 version.json 失败: HTTP ${resp.status}`)
      return
    }
    const remoteVersion = await resp.json()
    const remoteBuildId = remoteVersion.buildId

    // [2] 对比本地存储的 buildId
    const localBuildId = localStorage.getItem(SW_VERSION_KEY)

    if (localBuildId && localBuildId !== remoteBuildId) {
      // ----------------------------------------------------------
      // 版本变化 —— 用户正在用旧 SW, 里面 precache 了旧 index.html
      // 这个旧 SW 会拦截导航 → 返回旧 index.html → 引用被删的旧 chunk → 404
      // 所以必须: 先干掉旧 SW + 清所有 caches, 再 reload 拿新版本
      // 防御: 最多自动 reload 1 次 — 若 reload 后版本再次不匹配
      // (说明部署目录被回写/不一致), 直接写入远端 buildId 并继续注册,
      // 绝不循环刷新把用户页面打死
      // ----------------------------------------------------------
      const reloadCount = Number(localStorage.getItem(SW_RELOAD_COUNT_KEY) || '0')
      if (reloadCount >= 1) {
        console.warn(`[SW] 清理 reload 后版本仍不匹配 (local=${localBuildId} remote=${remoteBuildId}), 不再刷新, 直接采用远端版本`)
        localStorage.setItem(SW_VERSION_KEY, remoteBuildId)
        localStorage.removeItem(SW_RELOAD_COUNT_KEY)
        // fall through 到下面的注册逻辑
      } else {
      console.warn(`[SW] 检测到新版本: ${localBuildId} → ${remoteBuildId}, 正在清理...`)

      // 1. unregister 所有 SW
      const registrations = await navigator.serviceWorker.getRegistrations()
      for (const reg of registrations) {
        await reg.unregister()
      }

      // 2. 删除所有 CacheStorage (包括 workbox precache + runtime cache)
      //    这样 reload 后不会命中任何旧缓存
      const cacheNames = await caches.keys()
      await Promise.all(cacheNames.map(name => caches.delete(name)))

      // 3. 清 localStorage 里的版本标记
      //    关键: SW_VERSION_KEY 也必须清掉, 否则 reload 后 localBuildId 仍是旧值,
      //    永远走不进注册分支 → 无限 reload 循环
      localStorage.removeItem(SW_REGISTERED_KEY)
      localStorage.removeItem(SW_VERSION_KEY)
      localStorage.setItem(SW_RELOAD_COUNT_KEY, String(reloadCount + 1))

      // 4. 等 1 帧让 unregister 生效, 然后 reload (绕过 HTTP 缓存)
      await new Promise(r => setTimeout(r, 100))
      console.info('[SW] 旧版本已清理, 即将 reload 到新版本')
      location.reload()
      return // reload 后就不会执行下面的注册逻辑了
      }
    }

    // [3] 版本没变, 正常注册/更新 SW
    const registration = await navigator.serviceWorker.register('/sw.js', {
      // 关键: updateViaCache: 'none' — SW 文件永远走网络, 让 workbox 能及时发现新 precache manifest
      updateViaCache: 'none',
    })

    // 标记已注册 (帮助判断是否首次访问)
    localStorage.setItem(SW_VERSION_KEY, remoteBuildId)
    localStorage.setItem(SW_REGISTERED_KEY, '1')
    localStorage.removeItem(SW_RELOAD_COUNT_KEY)

    // [4] 监听 SW 更新事件 — 新 SW 下载完成后 skipWaiting 立即激活
    registration.addEventListener('updatefound', () => {
      const newSW = registration.installing
      if (!newSW) return
      console.info('[SW] 新版本下载中...')
      newSW.addEventListener('statechange', () => {
        if (newSW.state === 'installed' && navigator.serviceWorker.controller) {
          // 新 SW 准备好了, 立即激活 (不等所有 tab 关)
          console.info('[SW] 新版本已就绪, 正在 activate...')
          newSW.postMessage({ type: 'SKIP_WAITING' })
        }
      })
    })

    // 页面首次加载时也主动触发一次 SW 检测 (避开 HTTP 缓存)
    registration.update().catch(() => {})

    console.info(`[SW] 已注册, buildId=${remoteBuildId}`)
  } catch (err) {
    // SW 注册失败不应影响主流程 — 用户照样能正常用
    console.warn('[SW] 注册失败 (不影响功能):', err.message)
  }
}

// 不阻塞首屏渲染 — 等 load 事件后再执行
if (document.readyState === 'complete') {
  manageServiceWorker()
} else {
  window.addEventListener('load', manageServiceWorker, { once: true })
}
