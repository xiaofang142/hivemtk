import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config.js'

// Vitest 配置：单测 + 组件测试
// 复用 vite.config.js 的 alias 与插件配置
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      globals: true,
      environment: 'jsdom',
      // N-21（2026-11-12）：默认 5s testTimeout 在本机高负载（多 lane 并行构建/测试）
      // 下会误杀确定性完成的用例（clue_list_filter_push 20s 实跑 9/9 全绿=纯慢非悬挂，
      // settle 确定式等待已排除假绿）。容量兜底 15s——与 be969108「timeout 零放大」
      // 不冲突：那条纪律禁的是放大超时掩盖真悬挂，此处已实证非悬挂。
      testTimeout: 15000,
      include: ['tests/unit/**/*.{test,spec}.{js,mjs,ts}'],
      exclude: ['node_modules', 'dist', 'tests/e2e/**'],
      // 并行跑测试文件。恢复并行的前提已成立：所有组件用例的等待模式都换成了
      // tests/unit/settle-helper.js 的 settle()/settleBody()（渲染连续两轮相同即早停，
      // 对 setTimeout 驱动的防抖/轮询也等），不再有「刷 N 次 flushPromises 够不够」
      // 这种赌运气的写法 —— 那正是当年被迫串行的根因（同一棵树连跑三次，
      // 失败集合分别是 0 / 30 / 11 个，全是等待模式在抢 CPU 时不够用）。
      //
      // 若未来又出现随机红：先查等待模式（是不是绕回了裸 flushPromises / 固定次数连刷），
      // 而不是把 fileParallelism 关回 false —— 串行只是把这类假失败藏起来，代价是全量变慢。
      fileParallelism: true,
      coverage: {
        provider: 'v8',
        reporter: ['text', 'html', 'json-summary'],
        reportsDirectory: './coverage',
        include: [
          'src/utils/**',
          'src/api/backup.js',
          'src/api/order.js',
          'src/api/securityAudit.js',
          'src/views/backup/**',
          'src/views/order/**',
          'src/views/securityAudit/**'
        ],
        exclude: ['**/*.test.*', '**/*.spec.*', '**/__tests__/**']
      }
    }
  })
)
