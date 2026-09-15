/**
 * ESLint 配置（已启用）
 *
 * 状态：本文件即生效配置，`npm run lint:check`（= eslint .）直接使用。
 *       CI 接入见 .github/workflows/user-server-ci.yml 的 user-web-lint job（OPT-FE-01）。
 *
 * 历史：原注释写「参考用，尚未启用」，实际早已被 eslint 自动加载 —— 属注释与事实不符，
 *       于 OPT-FE-01 修复时一并更正。
 *
 * 设计目标：
 *   - 约束新增代码必须使用 `import { http } from '@/utils/request'`
 *   - 存量 43 个 `import request from '@/utils/request'` 文件通过 overrides 临时放行
 *     （等存量逐步迁移完成后删除 overrides 块，见 OPT-FE-03）
 *   - 不破坏现有 Vue 3 + Vite 构建
 */
import pluginVue from 'eslint-plugin-vue'
import js from '@eslint/js'
import globals from 'globals'

export default [
  js.configs.recommended,
  ...pluginVue.configs['flat/recommended'],

  // 全局规则
  {
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: 'module',
      globals: {
        // Browser 运行时全局变量（window 之外常用的标准 Web API）
        ...globals.browser,
        // Vite/Node 构建脚本环境（vite.config.js 等入口需要）
        ...globals.node,
      },
    },
    rules: {
      // P1-3: 禁止 default 导入 @/utils/request，新代码必须用 { http }
      // importNames: ['default'] —— 只禁 default 导入；{ http } 命名导入不受影响
      'no-restricted-imports': [
        'error',
        {
          paths: [
            {
              name: '@/utils/request',
              importNames: ['default'],
              message: "请使用 `import { http } from '@/utils/request'`，不要 default 导入。详见 request.js 顶部说明。",
              allowTypeImports: false,
            },
          ],
        },
      ],
      // Vue 3 <script setup> 不需要 import Vue
      'vue/multi-word-component-names': 'off',
      // 允许 console.warn / console.error（生产日志），警告 console.log
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      // 允许解构未使用变量（Vue 3 props 解构常见）
      'no-unused-vars': ['warn', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    },
  },

  // 存量放行：43 个文件继续使用 default 导入（等逐步迁移后删除此块）
  {
    files: [
      'src/api/operationLog.js',
      'src/api/churnPrediction.js',
      'src/api/feishu.js',
      'src/api/tuning.js',
      'src/api/license.js',
      'src/api/oneid.js',
      'src/api/obs.js',
      'src/api/integration.js',
      'src/api/dashboardScreen.js',
      'src/api/platform.js',
      'src/api/livecode.js',
      'src/api/batchOperation.js',
      'src/api/chatChannel.js',
      'src/api/userSegment.js',
      'src/api/objection.js',
      'src/api/customerService.js',
      'src/api/securityAudit.js',
      'src/api/aiAgent.js',
      'src/api/bulkMessaging.js',
      'src/api/i18nStats.js',
      'src/api/customerServiceAgent.js',
      'src/api/tiktokAutoReply.js',
      'src/api/customerEvent.js',
      'src/api/assetBundle.js',
      'src/api/material.js',
      'src/api/backup.js',
      'src/api/customerSession.js',
      'src/api/conversionFunnel.js',
      'src/api/glossary.js',
      'src/api/scriptTemplate.js',
      'src/api/dingtalkApp.js',
      'src/api/whatsappCloud.js',
      'src/api/abExperiment.js',
      'src/api/channelAgentBinding.js',
      'src/api/assetMarket.js',
      'src/api/marketingFlow.js',
      'src/api/telegram.js',
      'src/api/customer360.js',
      'src/api/customerJourney.js',
      'src/api/customReport.js',
      'src/api/aiProductivity.js',
      'src/api/tiktokCard.js',
      'src/api/persona.js',
    ],
    rules: {
      'no-restricted-imports': 'off',
    },
  },

  // 测试 / 构建配置文件放行
  {
    files: ['**/*.test.js', '**/*.spec.js', 'vite.config.js', 'vitest.config.*'],
    rules: {
      'no-restricted-imports': 'off',
      // Playwright 的 test 回调首参是 fixture 容器：`async ({}, testInfo) => {}`
      // 是官方写法，表示"本用例不注入任何 fixture"。该空对象模式由 Playwright
      // 运行时按解构出的属性名决定注入哪些 fixture，改写为具名参数会改变注入语义。
      // 属规则与框架约定的冲突，此处显式豁免（仅限测试文件）。
      'no-empty-pattern': 'off',
    },
  },

  // Chrome 扩展环境（OPT-FE-01 修复）
  // browser_automation / bridge 是 Chrome 扩展，使用 chrome.* 扩展 API。
  // 原配置只提供 globals.browser + globals.node，导致 26 处
  // `'chrome' is not defined` 误报（no-undef）。
  {
    files: ['browser_automation/**/*.js', 'bridge/**/*.js'],
    languageOptions: {
      globals: {
        ...globals.webextensions,
      },
    },
  },

  // 忽略目录
  {
    ignores: [
      'dist/**',
      '**/dist/**',
      'node_modules/**',
      'public/**',
      'src/types/components.d.ts',
      // 本文件是配置模板副本，非业务源码，不应被当作待 lint 的模块解析
      'eslint.config.recommended.mjs',
    ],
  },
]
