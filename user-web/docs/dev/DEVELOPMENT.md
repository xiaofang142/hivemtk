# user-web 代码开发手册

> **规则级别**: ⭐⭐ 项目级开发文档

本手册面向 `user-web` 工程的所有开发与联调场景，覆盖环境搭建、启动命令、目录导航、新增页面/API 标准流程、国际化、Element Plus 最佳实践、测试、构建部署与调试技巧。所有命令与路径均取自仓库实际配置。

## 1. 环境准备

### 1.1 前置要求

| 工具 | 版本 | 说明 |
| --- | --- | --- |
| Node.js | ≥ 20 | 推荐 LTS（20.x / 22.x），`vite.config.js` 使用 ESM 顶层 `import` |
| npm | ≥ 10 | 或 `pnpm 8+` / `yarn 4+`，仓库内置 `package-lock.json`，推荐 `npm install` |
| 后端 user-server | 任意版本 | 默认 `http://localhost:8204`，需先启动；E2E 测试要求两端口都能访问 |
| 浏览器 | Chrome ≥ 110 | Vue 3.5 + ESM + Element Plus 2.9 现代浏览器特性 |

### 1.2 安装依赖

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web
npm install
```

> 仓库已配置 `unplugin-auto-import` 与 `unplugin-vue-components`，无需手动注册 Element Plus 组件；图标按需导入由 `vite.config.js` 中的 `ElementPlusIconResolver` 处理。

### 1.3 .env.development 配置

`user-web/.env.development` 内容：

```bash
# 开发环境：使用相对路径，请求经 vite 代理(/api → http://localhost:8204)转发，
# 与首页 CSP(connect-src 'self') 兼容；勿改为绝对 http 地址，否则会被 CSP 拦截。
VITE_API_BASE_URL=/
```

| 变量 | 作用 | 默认 | 备注 |
| --- | --- | --- | --- |
| `VITE_API_BASE_URL` | axios 实例 baseURL | `/`（dev） | 生产构建可改绝对域名；运行期还会被 `localStorage.apiConfig.baseUrl` 覆盖 |
| WebSocket URL | 由 `configManager.getApiConfig()` 动态推导 | 同源 + `/api/ws/agent` 与 `/api/ws/visitor` | 不可单独配置 |

如需联调本地后端，保持默认即可（Vite 代理 `/api → localhost:8204`）。如需联调远程 demo：

```bash
# 修改 .env.development
VITE_API_BASE_URL=https://hiveuserapi.xapptool.cn
# 或者复制 .env.example
cp .env.example .env.development
```

## 2. 启动命令

| 命令 | 作用 | 备注 |
| --- | --- | --- |
| `npm run dev` | 启动 Vite dev server | 默认端口 **8211**，`strictPort:false` 端口占用时自动递增；`host:true` 允许外部访问；`open:false` 不自动开浏览器 |
| `npm run build` | 生产构建，输出到 `dist/` | 按要求分包：`vue/router/pinia` + `element-plus` + `axios` 三个独立 chunk |
| `npm run preview` | 预览构建产物（静态服务） | 用于本地验证生产包 |
| `npm run test` | 单元测试（Vitest，单次运行） | `vitest.config.js` 配置 |
| `npm run test:watch` | 监听模式 | 开发期间持续运行 |
| `npm run test:coverage` | 覆盖率报告 | `@vitest/coverage-v8` |
| `npm run test:e2e` | Playwright E2E | `playwright.config.js` baseURL 默认 `http://localhost:5173` |
| `npm run test:e2e:ui` | Playwright UI 模式 | 推荐：可视化调试用例 |
| `npm run test:e2e:report` | 查看 E2E HTML 报告 | 浏览器打开 `playwright-report/` |

```bash
# 启动开发
npm run dev
# 浏览器访问 http://localhost:8211
```

### 2.1 端口对照表

> **单一源**：`vite.config.js` 的 `server.port=8211` + `server.proxy['/api'].target='http://localhost:8204'`
> user-server 端对应字段：`user-server/internal/config/ports.go` `DefaultListenPort="8204"`

| 端口 | 服务 / 应用 | 启动入口 | 单一源 | 文档源 |
| --- | --- | --- | --- | --- |
| **8211** | **user-web**（Vite dev） | `npm run dev` | `vite.config.js server.port=8211` | `vite.config.js:102` |
| **8204** | **user-server**（被前端联调） | `cd ../user-server && go run ./cmd/api` | user-server `config.DefaultListenPort` | user-server/docs/dev/DEVELOPMENT.md §2.4 |
| 8202 | user-server PG（docker 宿主机映射） | `docker compose -f docker-compose.yml up -d` | user-server `config.DefaultDBPortDocker` | user-server docs §2.4 |
| 8203 | user-server Redis | `docker compose -f docker-compose.yml up -d` | user-server `config.DefaultRedisPort` | user-server docs §2.4 |
| 8205 | platform-server（被前端联调，可选） | `cd ../hivemtk-platform/platform-server && go run cmd/api/main.go` | user-server `config.DefaultPlatformPort` | user-server docs §2.4 |
| 8207 | LLM（本地推理） | user-server `make inference-host-up` | user-server `config.DefaultLLMPort` | user-server docs §2.4 |
| 8208 | Embedding（本地推理） | user-server `make inference-host-up` | user-server `config.DefaultEmbeddingPort` | user-server docs §2.4 |
| 8209 | Rerank（本地推理） | user-server `make inference-host-up` | user-server `config.DefaultRerankPort` | user-server docs §2.4 |
| 8232 | user-server PG（dev 本机直连） | `pg_ctl -D /usr/local/var/postgres start` | user-server `config.DefaultDBPortDev` | user-server `config.yaml` |
| 5173 | Playwright E2E 目标 URL | `npm run test:e2e` | `playwright.config.js baseURL` | `playwright.config.js` |
| 8204 | bridge 扩展 popup server URL 默认 | `http://localhost:8204` | `user-web/bridge/src/core/constants.js DEFAULT_USER_SERVER.port` | user-web/bridge/docs/dev/DEVELOPMENT.md §3 |

**前端启动约束**（禁软启动 / 禁多处硬编码）：

1. Vite 端口与代理目标均集中在 `vite.config.js`，**禁止在 `.env.*` 中覆盖**（`.env.development` 仅可覆盖 `VITE_API_BASE_URL` 用于生产跨域）
2. WS URL 由 `src/utils/configManager.js` 的 `getApiConfig()` 动态推导（`baseUrl + /api/ws/agent` 与 `/api/ws/visitor`），禁止前端代码直接写绝对 URL
3. E2E `baseURL` 5173 是 Playwright 配置独立项，**与 dev server 8211 解耦**；可通过 `E2E_BASE_URL` 环境变量覆盖

## 3. 目录导航

| 目录 | 作用 | 关键文件 |
| --- | --- | --- |
| `src/api/` | 业务 API 模块（78 个文件，按域拆分） | `knowledge.js`、`customer360.js`、`aiAgent.js` 等 |
| `src/components/` | 跨页面复用组件 | `PageHeader.vue`、`PageState.vue`、`{Platform}CardPreview.vue` |
| `src/constants/` | 业务枚举常量 | `status.js`、`role.js`、`source.js`、`msgType.js` 等 21 个 |
| `src/i18n/` | 国际化 | `locales/{zh,en,ja,ar}.json` + `index.js` + `locale.js` |
| `src/layout/` | 主布局 | `Layout.vue`（顶栏 + 侧边栏 + 主内容区） |
| `src/router/` | 路由配置 | `index.js`（懒加载守卫）+ `modules/*.js`（60+ 业务模块） |
| `src/stores/` | Pinia 状态 | `user.js` / `permission.js` / `app.js` / `material.js` |
| `src/styles/` | 全局 SCSS | `index.scss` / `reset.scss` / `variables.scss` |
| `src/types/` | 类型声明 | `components.d.ts`（自动生成） |
| `src/utils/` | 工具方法 | `request.js` / `configManager.js` / `agentSocket.js` / `chatSocket.js` / `initHelper.js` / `iconMap.js` / `list.js` / `map.js` |
| `src/views/` | 业务页面 | 60+ 子目录，按业务域组织 |
| `tests/e2e/` | E2E 测试 | `*.spec.js` + `audit/*.json` |
| `public/` | 静态资源 | `favicon.svg` |

## 4. 新增页面的标准流程

以新增「市场活动管理」为例：

### Step 1：注册路由模块

在 `src/router/modules/` 新增 `campaign.js`：

```js
// src/router/modules/campaign.js
export default [
  {
    path: 'campaign/list',
    name: 'CampaignList',
    component: () => import('@/views/campaign/List.vue'),
    meta: {
      title: '市场活动',
      group: 'reach',          // 决定侧边栏分组
      icon: 'Promotion',       // Element Plus 图标名（裸用）
      requiresAuth: true
    }
  }
]
```

> 路由 `path` 不要以 `/` 开头（自动挂在 `Layout` 下）；模块名需与 `router/index.js` 的 `moduleNames` 列表项一致，**新模块必须在 `moduleNames` 数组中追加**才能被懒加载守卫识别。

### Step 2：编辑 `src/router/index.js`

在 `moduleNames` 数组末尾追加 `'campaign'`。

### Step 3：编写视图

```vue
<!-- src/views/campaign/List.vue -->
<template>
  <div class="campaign-page">
    <PageHeader title="市场活动" :actions="[{label:'新建', icon:'Plus', onClick: onCreate}]" />
    <el-table :data="list" v-loading="loading" border>
      <el-table-column prop="name" label="活动名称" />
      <el-table-column prop="status" label="状态" />
    </el-table>
    <el-pagination ... />
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import PageHeader from '@/components/PageHeader.vue'
import { campaignAPI } from '@/api/campaign'

const list = ref([])
const loading = ref(false)

const loadList = async () => {
  loading.value = true
  try {
    list.value = await campaignAPI.list({ page: 1, size: 20 })
  } finally {
    loading.value = false
  }
}

const onCreate = () => { /* ... */ }
onMounted(loadList)
</script>
```

### Step 4：编写 API 文件（必须）

```js
// src/api/campaign.js
import { http } from '@/utils/request'   // 新代码必须用 { http }

export const campaignAPI = {
  list(params) { return http.get('/api/campaign/list', params) },
  create(data) { return http.post('/api/campaign', data) },
  update(id, data) { return http.put(`/api/campaign/${id}`, data) },
  remove(id) { return http.delete(`/api/campaign/${id}`) }
}

export default campaignAPI
```

### Step 5：注册菜单（MENU_SPEC.md）

参照 [../../MENU_SPEC.md](../ui-inventory/MENU_SPEC.md) 的结构在文档中追加菜单项；如需在 `Layout.vue` 的 `topMenus` 中挂菜单，编辑 `Layout.vue` 的菜单树（参考既有分组字段 `group: 'reach'`）。

### Step 6：补 i18n 文案

在 `src/i18n/locales/zh.json` 与 `en.json` 中追加 `menu.campaign` 与对应业务文案。

### Step 7：自测

- `npm run dev` 后访问 `http://localhost:8211/#/campaign/list` 应能正常加载
- 运行 `node check_menu.mjs` 校验菜单与路由一致性
- 若涉及多步骤交互，补充 `tests/e2e/campaign.spec.js`

## 5. 新增 API 调用的标准流程

### 5.1 文件结构

- 文件名按业务域命名，与后端 URL 一级路径对齐（如 `/api/customer/*` → `customer.js`）
- 统一放在 `src/api/`
- 每个文件 `export` 一个聚合对象（如 `customerAPI`）或多个具名函数

```js
// src/api/foo.js - 推荐写法
import { http } from '@/utils/request'

export const fooAPI = {
  list(params) { return http.get('/api/foo/list', params) },
  get(id) { return http.get(`/api/foo/${id}`) },
  create(data) { return http.post('/api/foo', data) },
  update(id, data) { return http.put(`/api/foo/${id}`, data) },
  remove(id) { return http.delete(`/api/foo/${id}`) },
  upload(formData) { return http.upload('/api/foo/upload', formData) }
}

export default fooAPI
```

### 5.2 axios 实例与拦截器

所有请求经 `src/utils/request.js` 创建的 axios 实例，**禁止在 api 文件或组件中直接 `import axios`**。拦截器链路：

| 拦截器 | 行为 |
| --- | --- |
| `request.use` | 注入 `Authorization: Bearer {token}`（来自 `localStorage.token`）+ `Accept-Language`（来自 `i18n/locale.getStoredLocale()`） |
| `response.use` 成功分支 | 兼容 `code === 'SUCCESS' \|\| 200 \|\| 0` → 返回 `data.data`；`responseType:'blob'` 直接透传 |
| `response.use` 失败分支 | 401 → 清 token 跳 `/login`；403/404/429/5xx → `ElMessage` 国际化提示；非 JSON 兜底（反向代理层 404 HTML）→ 提示"响应异常" |
| 业务跳转码 | `INIT_REQUIRED` → `redirectTo('/setup')`，可通过 `INIT_REDIRECT_MAP` 扩展 |
| `_silent` 标记 | 调用方传 `{ _silent: true }` 关闭统一 toast，自行处理错误 |

```js
// 静默调用示例（不弹默认 toast）
try {
  const data = await http.get('/api/foo/123', {}, { _silent: true })
} catch (err) {
  // 自行决定如何提示
  ElMessage.warning(err.message)
}
```

### 5.3 loading 管理

- 列表页推荐使用 `v-loading` 指令包裹表格/卡片
- 全局 loading 不要直接操作 DOM，应在组件内 `const loading = ref(false)` 控制
- 批量操作时建议用 `Promise.allSettled` + 进度条，避免单点失败阻塞

### 5.4 文件上传 / 下载

```js
// 上传
const formData = new FormData()
formData.append('file', file)
await http.upload('/api/material/upload', formData)

// 下载（responseType: 'blob' 走特殊分支，返回 Blob 而非业务码）
const blob = await http.post('/api/customReport/export', data, { responseType: 'blob' })
const url = URL.createObjectURL(blob)
const a = document.createElement('a')
a.href = url
a.download = 'report.xlsx'
a.click()
URL.revokeObjectURL(url)
```

## 6. 国际化（i18n）

### 6.1 资源位置

- 短语库：`src/i18n/locales/{zh,en,ja,ar}.json`
- 默认 + fallback 语言：`zh`
- 运行时获取：`src/i18n/locale.js` 的 `getStoredLocale()` 从 `localStorage.locale` 读取
- RTL 支持：`applyDirection(locale)` 根据语言切换 `<html dir="rtl">`（阿拉伯文）

### 6.2 使用方式

```vue
<script setup>
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
const title = t('menu.campaign')
</script>

<template>
  <span>{{ $t('layout.logout') }}</span>
</template>
```

### 6.3 key 命名约定

- 顶层命名空间：`menu.*` / `layout.*` / `system.*` / `http.*` / `common.*`
- 业务文案：`{module}.{field}`，如 `customer360.phone`

### 6.4 构建期预编译

`vite.config.js` 中 `@intlify/unplugin-vue-i18n` 在构建期将 JSON 编译为运行时函数，避免运行期 `new Function` 触发 CSP `script-src 'self'` 的 `unsafe-eval` 拦截。新增/修改文案直接编辑 JSON 即可，无需改 `i18n/index.js`。

## 7. 表单与表格最佳实践（Element Plus）

### 7.1 表单

- 使用 `<el-form ref="formRef" :model="form" :rules="rules">` 配合 `formRef.value.validate()` 校验
- `rules` 中校验函数返回 `Promise<void>` 以支持异步校验
- 提交按钮在 `loading` 期间禁用，避免重复提交

```vue
<el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
  <el-form-item label="名称" prop="name">
    <el-input v-model="form.name" maxlength="50" show-word-limit />
  </el-form-item>
  <el-form-item>
    <el-button type="primary" :loading="submitting" @click="onSubmit">保存</el-button>
  </el-form-item>
</el-form>
```

### 7.2 表格

- 列定义优先使用 `prop + label`，复杂渲染用 `<template #default="{ row }">`
- 分页用 `<el-pagination>`，`@current-change` 与 `@size-change` 触发重新加载
- 表头筛选 / 排序通过 `:filters` 与 `:sort-method` 实现
- 操作列固定在右侧 `<el-table-column fixed="right">`

```vue
<el-table :data="list" v-loading="loading" border @sort-change="onSort">
  <el-table-column prop="name" label="名称" sortable="custom" />
  <el-table-column label="状态">
    <template #default="{ row }">
      <el-tag :type="row.status === 'active' ? 'success' : 'info'">
        {{ row.status }}
      </el-tag>
    </template>
  </el-table-column>
  <el-table-column label="操作" fixed="right" width="180">
    <template #default="{ row }">
      <el-button text @click="onEdit(row)">编辑</el-button>
      <el-button text type="danger" @click="onRemove(row)">删除</el-button>
    </template>
  </el-table-column>
</el-table>
<el-pagination
  v-model:current-page="page.current"
  v-model:page-size="page.size"
  :total="page.total"
  @current-change="loadList"
  @size-change="loadList"
/>
```

### 7.3 通用辅助

- 空状态用 `<PageState empty />` 或 `<el-empty>` 统一展示
- 列表搜索表单可考虑抽到独立组件，便于跨页面复用
- 弹窗用 `<el-dialog v-model="visible" :title="title" width="600px">`，关闭时清空表单

## 8. 测试

### 8.1 单元测试（Vitest）

```bash
npm run test
npm run test:watch
npm run test:coverage
```

- 配置：`vitest.config.js`（环境 `jsdom`，覆盖率 `@vitest/coverage-v8`）
- 测试文件位置：`tests/unit/` 与组件同目录的 `*.spec.js`
- 推荐对 utils / store / 复杂计算函数写单元测试

### 8.2 E2E 测试（Playwright）

```bash
# 首次安装浏览器
npx playwright install

# 运行全部 E2E
npm run test:e2e

# UI 模式（可视化调试，强烈推荐）
npm run test:e2e:ui

# 查看报告
npm run test:e2e:report
```

**配置要点**（`playwright.config.js`）：

- `testDir: './tests'`，`testMatch: '**/*.spec.js'`
- `baseURL`：`process.env.E2E_BASE_URL || 'http://localhost:5173'`（注意是 5173 不是 dev server 的 8211）
- `workers: 1`，`fullyParallel: false`：串行执行避免数据竞争
- `timeout: 30s`，`actionTimeout: 15s`，`navigationTimeout: 20s`
- 失败时自动截图 + 录制视频 + trace
- `auth.setup.spec.js`：登录态前置 setup，所有依赖登录的用例共享 storage state

**E2E 用例编写建议**：

```js
// tests/e2e/customer360.spec.js
import { test, expect } from '@playwright/test'

test.describe('客户360', () => {
  test('列表可加载', async ({ page }) => {
    await page.goto('/#/customer360/list')
    await expect(page.getByRole('heading', { name: '客户 360' })).toBeVisible()
    await expect(page.locator('.el-table__row').first()).toBeVisible()
  })
})
```

### 8.3 check_menu.mjs 脚本

仓库根有 `check_menu.mjs` / `check_menu2.mjs` / `check_menu3.mjs` 三个巡检脚本，用于校验：

- 路由模块文件与 `moduleNames` 列表一致性
- 菜单配置（`Layout.vue` 中 `topMenus`）与实际路由是否对应
- `MENU_SPEC.md` 中记录的页面与代码中实际存在的视图组件是否匹配

```bash
node check_menu.mjs        # 基础校验
node check_menu2.mjs       # 二次校验（更严格）
node check_menu3.mjs       # 三次校验（含文档一致性）
```

**推荐**在每次新增 / 重命名路由后执行。

### 8.4 API 联调清单

`tests/` 目录下：

- `API_CHECKLIST.md`：所有后端接口清单
- `API_INVENTORY.json`：接口元数据
- `API_TEST_RESULTS.json`：联调结果记录
- `.api_id_cache.json`：ID 缓存（避免重复创建测试数据）

## 9. 构建与部署

### 9.1 构建命令

```bash
npm run build
# 产物输出到 dist/
```

**分包策略**（`vite.config.js` 中 `manualChunks`）：

| chunk 名 | 包含 |
| --- | --- |
| `vue` | `vue` + `vue-router` + `pinia` |
| `elementPlus` | `element-plus` |
| `utils` | `axios` |
| 业务代码 | 各路由模块 + 视图（按访问懒加载） |

`chunkSizeWarningLimit: 1600`，`sourcemap: false`（生产）。

### 9.2 预览

```bash
npm run preview
# 启动静态服务托管 dist/
```

### 9.3 Docker 集成

仓库根 `hivemtk/docker-compose.yml` 中以构建产物方式集成：

- **构建阶段**：`Dockerfile` 执行 `npm install && npm run build`
- **运行阶段**：由 同源托管 `dist/` 静态资源 + `/api` 反代到 `user-server:8204`

### 9.4 环境变量

| 文件 | 用途 | 关键变量 |
| --- | --- | --- |
| `.env.development` | 开发环境 | `VITE_API_BASE_URL=/` |
| `.env.production` | 生产构建 | 可配置独立域名 |
| `.env.example` | 模板 | 复制后改名 `.env.development` 使用 |
| `.development.example` / `.production.example` | 示例 | 同上 |

## 10. 调试技巧

### 10.1 Vue DevTools

- 安装 [Vue.js devtools](https://devtools.vuejs.org/) 浏览器扩展（支持 Vue 3）
- 查看 Pinia store 状态：DevTools → Pinia 标签页可实时修改 state
- 路由跳转历史在 Router 标签页查看

### 10.2 Network 面板

- 关注 `Authorization` header 是否注入（`utils/request.js` 请求拦截器）
- 401 时观察是否有跳转 `/login` 与 toast 提示
- 业务码非 `SUCCESS/200/0` 时检查 `data.message` 与 `data.code`
- WebSocket 在 Network → WS 标签页查看帧，关注 `seq` 与 `ack` 帧

### 10.3 Pinia 持久化

- `user` store 通过 `localStorage.token` + `localStorage.user_info` 持久化，刷新后 `initAuth()` 自动恢复
- `app` 与 `material` store 仅内存态，刷新后清空
- 调试时可在 Console 中执行：

```js
localStorage.getItem('token')
localStorage.getItem('user_info')
localStorage.getItem('apiConfig')        // 查看是否被覆盖了 baseURL
localStorage.getItem('agentSocket:lastSeq:1')   // 坐席 seq
sessionStorage.getItem('chatSocket:lastSeq:sessionId:visitorId')  // 访客 seq
```

### 10.4 路由懒加载问题

- 首次访问某路由 404：检查 `moduleNames` 数组是否包含模块名
- 路径首段 kebab-case 与模块名 camelCase 不匹配：在 `router/index.js` 的 `pathToModule` 中显式映射（如 `'asset-market' → 'assetMarket'`）
- `/system/*` 命名空间跨模块：`router.beforeEach` 已对 `system/` 自动加载 `ragProductConfig` / `systemUser` / `role` / `permission` 四个模块

### 10.5 i18n 调试

- 临时切语言：`localStorage.locale = 'en'` 后刷新
- 缺失 key 会显示原始 key（`missingWarn: false` 关闭了警告），调试期可临时改成 `true` 排查
- CSP 报错（`unsafe-eval`）：确认 `vite.config.js` 的 `VueI18n` 插件 `dropMessageCompiler: true`

### 10.6 WebSocket 调试

- 在 Console 创建临时连接：

```js
const sock = new WebSocket('ws://localhost:8204/api/ws/agent?agent_id=1&agent_name=test&token=' + localStorage.getItem('token'))
sock.onmessage = e => console.log(JSON.parse(e.data))
```

- seq 异常时清空 `localStorage` 中的 `agentSocket:lastSeq:*` 与 `sessionStorage` 中的 `chatSocket:lastSeq:*`

### 10.7 axios baseURL 调试

```js
// 查看当前实例 baseURL（http 包装对象不暴露 defaults，通过 updateRequestConfig 获取）
import { updateRequestConfig } from '@/utils/request'
const { baseURL } = await updateRequestConfig()
console.log(baseURL)

// 临时切换（需触发 updateRequestConfig）
localStorage.setItem('apiConfig', JSON.stringify({ baseUrl: 'https://hiveuserapi.xapptool.cn' }))
await updateRequestConfig()
```

## 11. 常见问题排查

| 现象 | 排查方向 |
| --- | --- |
| 页面整页白屏 | 检查 i18n 是否触发 CSP `unsafe-eval`；检查 `vue.config.js` `compatConfig` |
| 路由跳转后 404 | 确认 `moduleNames` 列表已包含模块；`pathToModule` 是否需要 kebab→camel 映射 |
| 接口 401 但未跳登录 | 检查 `_silent` 是否误传 true；确认 `clearAuthAndGoLogin` 中的 `redirectTo` 是否被覆盖 |
| toast 重复弹出 | 检查是否在 2.5s 内对相同 message 触发多次（`lastToastMsg` 去重逻辑） |
| 文件上传 415 | 确认是否使用 `http.upload(url, formData)` 自动注入 `multipart/form-data` |
| 文件下载乱码 | 确认 `responseType: 'blob'`，响应拦截器会跳过 JSON 解析直接透传 |
| E2E 用例 5173 端口无响应 | `playwright.config.js` 的 `baseURL` 默认 5173，dev server 是 8211；可通过 `E2E_BASE_URL` 环境变量覆盖 |
| `import axios` 报错 | 应使用 `import { http } from '@/utils/request'`，禁止直接 import axios |

## 关联文档

- [ARCHITECTURE.md](./ARCHITECTURE.md) - 架构图
- [CONVENTIONS.md](./CONVENTIONS.md) - 代码规范
- [FEATURES.md](./FEATURES.md) - 功能清单
- [../../README.md](../../README.md) - 项目说明
- [../../MENU_SPEC.md](../ui-inventory/MENU_SPEC.md) - 菜单页面规格清单
- [../ui-inventory/](../ui-inventory/) - UI 清单文件

---

最近更新日期: 2026-07-26
