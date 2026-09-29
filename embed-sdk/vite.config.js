import { defineConfig } from 'vite'
import { resolve } from 'path'

export default defineConfig({
  build: {
    lib: {
      entry: resolve(__dirname, 'src/widget.js'),
      name: 'MarketingChatWidget',
      formats: ['iife', 'esm'],
      fileName: (format) => `marketing-chat-widget.${format}.js`
    },
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        extend: true,
        exports: 'named'
      }
    },
    minify: true, // Vite 8/Rolldown 用 oxc 原生压缩器；'esbuild' 需 esbuild 作为可选依赖，已移除
    sourcemap: false,
    target: 'es2018'
  },
  server: {
    // 8214 固定（与 platform-contributor 8215 相邻），拒绝漂移
    port: 8214,
    strictPort: true,
    open: '/demo.html'
  }
})

