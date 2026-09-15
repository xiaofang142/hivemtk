import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import VueI18n from '@intlify/unplugin-vue-i18n/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { VitePWA } from 'vite-plugin-pwa'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import { resolve } from 'path'
import { writeFileSync, mkdirSync } from 'fs'

// ============================================================================
// PWA 彻底防旧页/404 方案
// ----------------------------------------------------------------------------
// 根因链：
//   旧 SW 拦截 HTML 导航 → 返回旧 precache 的 index.html
//   → 旧 index.html 引用旧 hash chunk → rsync 删旧 chunk → 404
//   → 用户看到白屏/功能不全
//
// 四层防御：
//   Layer 1: 不让 SW 拦截 HTML 导航（navigateFallback + NavigationRoute 全部移除）
//            index.html 永远走网络（HTTP Cache-Control: no-cache）
//            静态资源 (JS/CSS/图片) 继续 precache — hash 命名天然无冲突
//   Layer 2: skipWaiting + clientsClaim — 新 SW 构建后立即 activate, 不等 tab 关
//   Layer 3: buildId 写入 version.json — 每次构建生成新 buildId, 写入
//            dist/version.json 和 sw.js 头部, 可用于版本探测
//   Layer 4: rsync 不删 assets/ — 保留旧 chunk, 即使极端情况也不会 404
// ============================================================================

// 每次构建生成唯一 buildId (git commit hash + 时间戳)
function generateBuildId() {
  try {
    const { execSync } = require('child_process')
    const gitHash = execSync('git rev-parse --short HEAD', { encoding: 'utf-8' }).trim()
    return `${gitHash}-${Date.now()}`
  } catch {
    return `build-${Date.now()}`
  }
}
const BUILD_ID = generateBuildId()

// 自定义插件: 构建时生成 dist/version.json
// main.js 启动时 fetch 这个文件对比 localStorage 里的版本
// 发现 buildId 变化 → unregister SW → 清 cache → reload
const versionJsonPlugin = {
  name: 'generate-version-json',
  closeBundle() {
    try {
      mkdirSync('dist', { recursive: true })
    } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    writeFileSync(
      'dist/version.json',
      JSON.stringify({
        buildId: BUILD_ID,
        buildTime: new Date().toISOString(),
        url: './index.html',
      }, null, 2),
      'utf-8'
    )
    console.log(`\n[version.json] buildId = ${BUILD_ID}\n`)
  },
}

// =============================================================
// 单一源约束（user-web / user-server 端口对齐）
// 单一代码源：user-server/internal/config/ports.go
// 单一文档源：user-server/docs/dev/DEVELOPMENT.md §2.4 端口对照表
// 跨包对齐：
//   - user-server DefaultListenPort="8204"（本文件 proxy target）
//   - user-web 自身 dev port=8211（本文件 server.port）
//   - bridge 端 src/core/constants.js DEFAULT_USER_SERVER.port=8204
// 严禁"软启动"——所有端口字面量必须与 ports.go / DEVELOPMENT.md §2.4 严格一致。
// =============================================================

// Element Plus 图标按需自动导入 resolver
//
// 背景：ElementPlusResolver 默认仅匹配 `El*` 前缀组件与 `ElIcon*` 前缀图标,
// 不处理模板里裸用的 `<Edit />`、`<CircleCloseFilled />` 等图标组件名。
// 此处构造一个自定义 resolver,在编译期把模板里裸用的图标名解析为
// `import { IconName } from '@element-plus/icons-vue'`,实现"按需导入"。
//
// 与 main.js 中删除的全量 `app.component(key, component)` 注册循环等价,
// 但仅注入实际被模板引用的图标,显著减小首屏 bundle 体积。
//
const epIconNames = new Set(Object.keys(ElementPlusIconsVue))
function ElementPlusIconResolver(name) {
  // 仅当 name 是 @element-plus/icons-vue 导出的图标名时才解析
  // 跳过 El 前缀(由 ElementPlusResolver 处理)、跳过小写开头(自定义组件)
  if (!/^[A-Z][a-zA-Z0-9]+$/.test(name)) return
  if (!epIconNames.has(name)) return
  return {
    name,
    from: '@element-plus/icons-vue',
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    // 自定义: 构建时生成 dist/version.json (main.js 用来探测版本)
    versionJsonPlugin,
    // 预编译 i18n 资源（./src/i18n/locales/*.json），运行期不再编译消息，
    // 避免 vue-i18n 用 new Function 触发 CSP script-src 'self' 的 unsafe-eval 拦截。
    // runtimeOnly 默认 true：改用 vue-i18n 运行时构建（无消息编译器）。
    // strictMessage:false 因为部分翻译含 <g>/<x>/<string> 等占位标签，按字面量保留。
    VueI18n({
      include: resolve(__dirname, './src/i18n/locales/**'),
      strictMessage: false,
      dropMessageCompiler: true,
    }),
    vue({
      compilerOptions: {
        // 禁用深度选择器弃用警告
        isCustomElement: () => false,
        whitespace: 'condense',
        // 添加自定义编译器选项以禁用警告
        compatConfig: {
          MODE: 3
        }
      }
    }),
    // Element Plus 按需自动导入：
    //   - 图标(裸用 <Edit /> 等): 由 ElementPlusIconResolver 解析,从 @element-plus/icons-vue 注入 import
    //   - 组件(<ElXxx />): 由 ElementPlusResolver 解析(此处仍保留 app.use(ElementPlus) 全量注册,
    //     两者并存不冲突,组件自动导入仅作降级路径)
    //   - importStyle:false: 不自动注入组件 CSS(仍走 main.js 中的 'element-plus/dist/index.css' 全量样式,
    // 待 后续阶段再切换为按需 CSS
    // 参考: https://element-plus.org/en-US/guide/quickstart.html#on-demand-import
    Components({
      resolvers: [
        ElementPlusIconResolver,
        ElementPlusResolver({
          importStyle: false,
        }),
      ],
      dts: 'src/types/components.d.ts',
    }),
    // ==========================================================================
    // PWA v2 — 彻底解决旧页面/404 问题
    // --------------------------------------------------------------------------
    // 关键改动:
    //   1. 移除 navigateFallback / NavigationRoute — HTML 导航不走 SW, 永远走网络
    //      (index.html 已 Cache-Control: no-cache, 每次都拿到最新版)
    //   2. 移除 additionalManifestEntries 里的 HTML 路由 — 不 precache 任何 HTML
    //      静态资源 (JS/CSS/图片) 继续 precache — hash 命名天然无冲突
    //   3. skipWaiting + clientsClaim — 新 SW 立即 activate, 不等用户关 tab
    //   4. injectRegister: false — main.js 自己写 SW 注册 + 版本检测逻辑
    //   5. manifest 用动态 buildId 做 revision — 每次构建触发 SW 更新
    //
    // 这样: 旧 SW 不会拦截导航 → 不会返回旧 index.html → 不会引用被删的旧 chunk → 不会 404
    // ==========================================================================
    VitePWA({
      registerType: 'prompt',            // main.js 手动注册 SW
      injectRegister: false,             // 关闭自动注入 sw-register.js
      manifest: {
        name: 'AI营销套件',
        short_name: 'HiveMtk',
        description: 'HiveMtk 企业级 AI 智能体客服平台',
        start_url: '/',
        display: 'standalone',
        background_color: '#ffffff',
        theme_color: '#409EFF',
        // 加 buildId 让 manifest 每次构建都变, 触发 SW 重新 precache
        buildId: BUILD_ID,
        icons: [
          { src: '/favicon.svg', sizes: 'any', type: 'image/svg+xml' },
        ],
      },
      devOptions: { enabled: false },
      workbox: {
        // ---- 关键: 不 precache 任何 HTML, 不让 SW 拦截导航 ----
        // navigateFallback: null — 显式禁用 NavigationRoute
        // workbox 默认会生成 navigateFallback:'/index.html' 的 NavigationRoute,
        // 必须显式 null 才能干掉它。否则旧 SW 仍会拦截导航 → 返回旧 index.html → 404。
        navigateFallback: null,
        // 静态资源继续 precache (hash 命名, 新旧共存不冲突)
        globPatterns: ['**/*.{js,css,svg,png,woff2,webp}'],
        // 排除 HTML 和 manifest, 这俩走网络
        globIgnores: ['**/*.html', 'manifest.webmanifest', 'version.json'],
        // skipWaiting + clientsClaim: 新 SW 构建后立即 activate
        // 不等所有 tab 关闭, 也不用等用户下次导航
        skipWaiting: true,
        clientsClaim: true,
        // 每次构建动态变更 precache manifest (buildId 作为缓存键)
        // 让 workbox 正确区分新旧版本
        additionalManifestEntries: [
          // version.json 也 precache, 但 main.js 会用 cache:no-store 绕过
          // (留作离线 fallback)
          { url: 'version.json', revision: BUILD_ID },
        ],
        cleanupOutdatedCaches: true,
        runtimeCaching: [
          // 图片: StaleWhileRevalidate (先显示缓存, 后台更新)
          {
            urlPattern: ({ request }) => request.destination === 'image',
            handler: 'StaleWhileRevalidate',
            options: {
              cacheName: 'images-v1',
              expiration: { maxEntries: 120, maxAgeSeconds: 60 * 60 * 24 * 30 },
            },
          },
          // API 静态资源: CacheFirst
          {
            urlPattern: ({ url }) => url.pathname.startsWith('/api/static/'),
            handler: 'CacheFirst',
            options: {
              cacheName: 'api-static-v1',
              expiration: { maxEntries: 64, maxAgeSeconds: 60 * 60 * 24 * 7 },
            },
          },
          // 字体文件: CacheFirst
          {
            urlPattern: ({ request }) =>
              request.destination === 'font' ||
              /\.(woff2?|ttf|eot|otf)$/.test(request.url),
            handler: 'CacheFirst',
            options: {
              cacheName: 'fonts-v1',
              expiration: { maxEntries: 32, maxAgeSeconds: 60 * 60 * 24 * 90 },
            },
          },
        ],
      },
    }),
  ],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
    },
  },
  css: {
    preprocessorOptions: {
      scss: {
        // 全局自动注入变量 —— 所有 <style lang="scss"> 块可直接用 $primary-color 等
        // 使用 @ 别名路径，确保无论文件在哪都能正确解析
        additionalData: '@import "@/styles/variables.scss";',
      }
    },
    // 添加CSS配置以处理深度选择器警告
    devSourcemap: true
  },
  // 添加构建选项以禁用警告
  build: {
    outDir: 'dist',
    sourcemap: false,
    chunkSizeWarningLimit: 1600,
    rollupOptions: {
      output: {
        manualChunks: {
          vue: ['vue', 'vue-router', 'pinia'],
          elementPlus: ['element-plus', '@element-plus/icons-vue'],
          echarts: ['echarts'],
          tinymce: ['tinymce', '@tinymce/tinymce-vue'],
          utils: ['axios', 'dompurify']
        }
      }
    }
  },
  // 添加服务器配置以禁用警告
  server: {
    port: 8211,
    strictPort: false, // 端口被占用时自动递增
    host: true, // 允许外部访问
    allowedHosts: true, // 允许 frp 反代的外部域名访问
    open: false, // 不自动打开浏览器
    hmr: {
      overlay: false
    },
    proxy: {
      '/api': {
        target: 'http://localhost:8204',
        changeOrigin: true,
        secure: false,
        ws: true
      },
      '/files': {
        target: 'http://localhost:8204',
        changeOrigin: true,
        secure: false
      }
    }
  },
  // 添加优化选项
  optimizeDeps: {
    // dompurify / tinymce / echarts 等是被懒加载（动态 import）或较大体积的依赖，
    // 若不在启动期预打包，首次被页面请求时 Vite 会临时优化，期间对该依赖的请求返回
    // 504，导致 import 该依赖的页面（sms/Jobs、chatChannel/List、email/Drafts、
    // whatsappBot/BulkMessaging 等）白屏。显式 include 使其在 server 启动期完成预构建。
    include: ['vue', 'vue-router', 'pinia', 'element-plus', 'axios', 'dompurify', 'echarts', 'tinymce', '@tinymce/tinymce-vue']
  },
  // 添加自定义配置以禁用警告
  define: {
    __VUE_OPTIONS_API__: true,
    __VUE_PROD_DEVTOOLS__: false,
    'process.env': {}
  }
})
