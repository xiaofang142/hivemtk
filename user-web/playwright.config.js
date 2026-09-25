import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright E2E 测试配置
 * 基线：前后端联合测试，不使用 mock 数据
 * 前端: http://localhost:8211（user-web 的 vite dev 端口，取自 vite.config.js）
 * 后端: http://localhost:8204 (user-server)
 */
export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.spec.js',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: [
    ['list'],
    ['html', { outputFolder: 'playwright-report', open: 'never' }]
  ],
  timeout: 30 * 1000,
  expect: { timeout: 10 * 1000 },
  use: {
    baseURL: process.env.E2E_BASE_URL || 'http://localhost:8211',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 15 * 1000,
    navigationTimeout: 20 * 1000
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] }
    }
  ]
})
