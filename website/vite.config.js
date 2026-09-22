import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'


// =============================================================
// 单一源约束（website 子路径 / dev 端口）
// 单一代码源：本文件的 SITE_BASE 与 server.port。
//   - dev-server.cjs 的 SITE_BASE / PORT 必须与本文件字面一致
//   - index.html 的 canonical/hreflang 前缀、README 的访问地址跟着 SITE_BASE
// 站点托管在 GitHub Pages 的项目子路径下，全部资源 URL 必须带这个前缀。
// 历史上这里把 /public 与 /merchant-api 反代到 platform-server:8205 供客服浮标拉联系
// 信息；浮标随线上域名下线一起删除后，官网运行期零后端依赖，不再有跨包端口对齐关系。
// =============================================================
const SITE_BASE = '/hivemtk/'

export default defineConfig({
  base: SITE_BASE,
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  // 纯静态站：不代理任何后端。历史版本在此把 /public 与 /merchant-api 反代到 platform-server:8205
  // 供右下角客服浮标拉联系信息；浮标随线上域名下线一起删除后，官网运行期零后端依赖。
  // dev 端口 8213 与 dev-server.cjs 的 PORT 是一对（仅本机预览用，不与任何后端端口对齐）。
  server: {
    port: 8213,
    host: true,
  },
  build: {
    sourcemap: false,
    chunkSizeWarningLimit: 1000,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (
              id.includes('vue') ||
              id.includes('vue-router') ||
              id.includes('vue-i18n')
            ) {
              return 'vendor'
            }
          }
        },
      },
    },
  },
})

