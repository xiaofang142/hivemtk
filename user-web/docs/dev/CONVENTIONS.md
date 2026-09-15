# user-web 代码规范

> **规则级别**: ⭐⭐ 项目级开发文档

本规范基于 `user-web` 实际代码与 `eslint.config.recommended.mjs`、`vite.config.js` 中已落地的约定梳理而成，覆盖命名、目录、组件、状态管理、API、路由、样式、i18n、安全、性能与提交规范。所有规则均与现有代码对齐，新增 / 重构代码必须遵循。

## 1. 命名规范

### 1.1 组件名

- **PascalCase**：所有 `.vue` 单文件组件的文件名与模板使用名采用 PascalCase
- 例：`PageHeader.vue`、`MaterialSelectDialog.vue`、`AgentBindingDialog.vue`、`DouyinCardPreview.vue`

```vue
<!-- ✅ 推荐 -->
<PageHeader title="客户360" />
<MaterialSelectDialog v-model:visible="dialogVisible" />

<!-- ❌ 禁止 -->
<page-header />            <!-- 全小写 -->
<materialSelectDialog />   <!-- camelCase 直接用 -->
```

### 1.2 文件名

| 类型 | 命名风格 | 示例 |
| --- | --- | --- |
| `.vue` 单文件组件 | PascalCase | `Layout.vue`、`MaterialSelectDialog.vue` |
| `src/api/*.js` | camelCase（业务域） | `customer360.js`、`aiAgent.js`、`livecode.js`、`tagSegmentation.js` |
| `src/router/modules/*.js` | camelCase（与 view 目录同名） | `customer360.js`、`douyinCard.js` |
| `src/stores/*.js` | camelCase | `user.js`、`permission.js`、`material.js` |
| `src/utils/*.js` | camelCase | `request.js`、`configManager.js`、`agentSocket.js` |
| `src/constants/*.js` | camelCase | `msgType.js`、`cardPlatform.js`、`leadStatus.js` |
| `src/i18n/locales/*.json` | 语言代码 | `zh.json` / `en.json` / `ja.json` / `ar.json` |
| `tests/e2e/*.spec.js` | kebab-case 或 camelCase | `auth.setup.spec.js`、`autoreply_e2e.spec.js` |

> **禁止**：文件名带版本后缀（`_v1` / `_v2`）、时间戳后缀（`_20250726`）、临时标记（`_new` / `_tmp` / `_bak`）。

### 1.3 变量名

- 局部变量：`camelCase`，例：`isLoading`、`currentPage`、`pageSize`
- `ref` 命名：与语义一致，不加 `Ref` 后缀（除非有必要消歧义）
- 常量：`SCREAMING_SNAKE_CASE`，例：`MAX_RECONNECT_DELAY_MS`、`INITIAL_RECONNECT_DELAY_MS`、`PING_INTERVAL_MS`、`STORAGE_KEY_PREFIX`

```js
// ✅ 推荐
const MAX_RECONNECT_ATTEMPTS_DEFAULT = 10   // utils/agentSocket.js
const PING_INTERVAL_MS = 25000
const apiBaseUrl = import.meta.env?.VITE_API_BASE_URL || ''

// ❌ 禁止
const max_reconnect_attempts = 10   // snake_case 用作局部变量
const apiBaseUrl2 = '...'          // 数字后缀
```

### 1.4 CSS 类名

- 采用 **BEM** 风格（block__element--modifier）
- 命名风格：kebab-case
- 全局类加项目前缀避免冲突（如 `app-` 前缀用于布局类）

```scss
// ✅ 推荐
.dashboard-screen-page { }      // block
.dashboard-header { }            // element
.kpi-card--active { }            // modifier
.app-aside.is-collapsed { }      // 状态类用 is-* 前缀

// ❌ 禁止
.dashboardScreenPage { }         // camelCase
.dashboard_screen_page { }       // snake_case
```

## 2. 目录规范

### 2.1 划分原则

| 目录 | 内容 | 不允许放 |
| --- | --- | --- |
| `src/api/` | 后端 API 调用封装，按业务域拆文件 | 业务逻辑、组件、UI |
| `src/views/` | 业务页面，按业务域分子目录（一个功能一个目录） | 公共组件、纯逻辑工具 |
| `src/components/` | 跨页面复用组件（≥2 个页面用到） | 仅一处使用的组件（应放对应 `views/{module}/components/`） |
| `src/stores/` | Pinia store，按业务域拆文件 | DOM 操作、API 直接调用 |
| `src/utils/` | 通用工具函数 | 业务逻辑、与具体页面耦合的辅助 |
| `src/router/` | 路由配置 + 模块懒加载 | 业务组件 |
| `src/i18n/` | 国际化资源与实例 | 业务逻辑 |
| `src/constants/` | 业务枚举常量（一类一文件） | 含逻辑的函数（除纯映射） |
| `src/styles/` | 全局 SCSS（变量、reset、index） | 页面级样式（应在组件 `<style scoped>`） |
| `src/layout/` | 主布局（Layout.vue） | 业务页面 |

### 2.2 视图目录组织

每个业务模块在 `src/views/` 下建独立子目录，内部按页面拆 `.vue`：

```text
src/views/customer360/
├── List.vue              # 列表页
├── Detail.vue            # 详情页（如有）
├── Edit.vue              # 编辑页（如有）
└── components/           # 模块私有子组件（如有）
    └── CustomerTagEditor.vue
```

复杂模块（如 `KnowledgeWorkspace/`）按子功能拆多文件：

```text
src/views/KnowledgeWorkspace/
├── KnowledgeManagement.vue
├── ChunkManagement.vue
├── ChunkEditor.vue
├── Playground.vue
├── ApiToken.vue
├── BatchImport.vue
├── ExternalImport.vue
├── OpenAPIIntegration.vue
├── KnowledgeStatistics.vue
└── FeedbackList.vue
```

### 2.3 公共组件判定

- 仅 1 处使用 → 放对应 `views/{module}/components/` 下
- ≥2 处使用 → 提升到 `src/components/`
- 跨页面弹窗统一放 `src/components/dialogs/`

## 3. Vue 组件规范

### 3.1 必须使用 Composition API + `<script setup>`

```vue
<!-- ✅ 推荐 -->
<script setup>
import { ref, computed, onMounted } from 'vue'
import PageHeader from '@/components/PageHeader.vue'
import { fooAPI } from '@/api/foo'

const list = ref([])
const loading = ref(false)
const total = computed(() => list.value.length)

const loadList = async () => {
  loading.value = true
  try {
    list.value = await fooAPI.list()
  } finally {
    loading.value = false
  }
}

onMounted(loadList)
defineExpose({ loadList })
</script>
```

```vue
<!-- ❌ 禁止：Options API -->
<script>
export default {
  data() { return { list: [] } },
  methods: { async loadList() { /* ... */ } },
  mounted() { this.loadList() }
}
</script>
```

### 3.2 props 定义

- 使用 `defineProps` + 对象类型声明
- 必填字段必须 `required: true`
- 复杂类型用 `Object as () => FooItem` 而非裸 `Object`

```js
const props = defineProps({
  visible: { type: Boolean, default: false },
  title: { type: String, required: true },
  data: { type: Object as () => CustomerItem, default: () => ({}) }
})
```

### 3.3 emits 声明

```js
const emit = defineEmits(['update:visible', 'confirm', 'cancel'])

const onConfirm = () => emit('confirm', { id: 1, name: 'foo' })
```

### 3.4 模板规范

- 自闭合标签：`<PageHeader />`（无内容时）
- 指令缩写：`v-bind:` → `:`，`v-on:` → `@`，`v-slot:` → `#`
- `v-for` 必须配 `:key`，且 key 唯一
- `v-if` 与 `v-for` 不要同时用在一个元素

```vue
<!-- ✅ 推荐 -->
<PageHeader :title="title" @refresh="loadList" />
<el-table-column v-for="col in columns" :key="col.prop" :prop="col.prop" :label="col.label" />

<!-- ❌ 禁止 -->
<div v-if="showList" v-for="item in list" :key="item.id">{{ item.name }}</div>
```

### 3.5 样式作用域

- 默认使用 `<style scoped lang="scss">`
- 需穿透子组件样式时用 `:deep()`，避免 `::v-deep` 与 `/deep/`
- 全局样式只在 `src/styles/index.scss` 引入

```vue
<style scoped lang="scss">
.campaign-page {
  &__header { padding: 12px 0; }
  &__table { margin-top: 16px; }
}
:deep(.el-table__row) {
  height: 48px;
}
</style>
```

## 4. 状态管理规范

### 4.1 Pinia store 命名

- 文件名：`{业务域}.js`，如 `user.js` / `permission.js` / `material.js` / `app.js`
- store id：与文件名一致，`defineStore('user', () => {...})`、`defineStore('permission', ...)`
- 使用处：`useUserStore` / `usePermissionStore` / `useMaterialStore` / `useAppStore`（与 id 对应的 camelCase）

### 4.2 state / getters / actions 划分

采用 Composition 风格 `defineStore(id, () => { ... })`：

- **state** → `ref()`
- **getters** → `computed()`
- **actions** → 普通函数

```js
// ✅ 推荐：参考 stores/user.js
export const useUserStore = defineStore('user', () => {
  const userInfo = ref({ id: '', username: '', email: '', role: '' })  // state
  const token = ref('')                                                  // state
  const isLoggedIn = computed(() => !!token.value)                       // getter
  const isAdmin = computed(() => userInfo.value.role === 'admin')        // getter

  const setToken = (newToken) => {                                       // action
    token.value = newToken
    if (newToken) localStorage.setItem('token', newToken)
    else localStorage.removeItem('token')
  }

  return { userInfo, token, isLoggedIn, isAdmin, setToken }
})
```

### 4.3 避免直接修改 state

- 组件中**不要**直接 `userStore.token = 'xxx'`
- 应通过 action 修改：`userStore.setToken('xxx')`
- 读取 state 用 `storeToRefs` 保证响应性，避免解构丢失响应性

```js
// ✅ 推荐
import { useUserStore } from '@/stores/user'
import { storeToRefs } from 'pinia'
const userStore = useUserStore()
const { role, isLoggedIn } = storeToRefs(userStore)   // 响应式解构
userStore.setToken(newToken)                            // action 修改

// ❌ 禁止
userStore.token = newToken                  // 直接改 state
const { role } = userStore                  // 解构丢失响应性（无 storeToRefs）
```

### 4.4 store 间依赖

- 显式 import 依赖 store，在 setup 内调用：`const userStore = useUserStore()`
- 通过 `storeToRefs(userStore)` 拿响应式引用，避免循环依赖
- 参考 `stores/permission.js` 对 `stores/user.js` 的依赖

### 4.5 持久化

- 仅 `user` store 持久化（token + user_info → localStorage）
- 其他 store 默认内存态，刷新清空
- 如需新增持久化，**不要**引入 `pinia-plugin-persistedstate`，按现有 `localStorage.setItem` 手动管理（保持依赖最小化）

## 5. API 调用规范

### 5.1 文件结构

```js
// src/api/campaign.js
import { http } from '@/utils/request'    // 新代码必须用 { http }

export const campaignAPI = {
  list(params) { return http.get('/api/campaign/list', params) },
  get(id) { return http.get(`/api/campaign/${id}`) },
  create(data) { return http.post('/api/campaign', data) },
  update(id, data) { return http.put(`/api/campaign/${id}`, data) },
  remove(id) { return http.delete(`/api/campaign/${id}`) }
}

export default campaignAPI
```

### 5.2 请求方法约定

| 方法 | 用途 | 参数形式 |
| --- | --- | --- |
| `http.get(url, params)` | 列表 / 详情 | 扁平 params 对象，**不要** `{ params }` 二次包裹 |
| `http.post(url, data)` | 创建 / 操作 | data 作为请求体 |
| `http.put(url, data)` | 全量更新 | data 作为请求体 |
| `http.delete(url, params)` | 删除 | params 作为 query |
| `http.upload(url, formData)` | 文件上传 | 自动注入 `multipart/form-data` |

### 5.3 响应处理

- 拦截器已剥离 `{ code, data, message }` 信封，调用方拿到的就是 `data.data`
- 业务错误由拦截器统一 `ElMessage` 提示，**不要**在调用方重复 `try/catch` 弹 toast
- 需要静默调用时传 `{ _silent: true }`，自行处理错误

```js
// ✅ 推荐：业务层不重复处理 toast
async function loadList() {
  loading.value = true
  try {
    list.value = await campaignAPI.list({ page: 1, size: 20 })
  } finally {
    loading.value = false
  }
}

// ✅ 静默调用（如轮询、表单校验异步校验）
try {
  const exists = await http.get('/api/campaign/check-name', { name }, { _silent: true })
  if (exists) formErrors.name = '名称已存在'
} catch (e) { /* 忽略 */ }
```

### 5.4 错误码处理

| 状态码 / 业务码 | 处理 |
| --- | --- |
| `SUCCESS` / `200` / `0` | 拦截器返回 `data.data` |
| `401` | 清 token + 跳 `/login` + 提示「登录已过期」 |
| `403` | 提示「无访问权限」 |
| `404` | 提示「资源不存在」 |
| `429` | 提示「请求过于频繁」 |
| `5xx` | 提示「服务器开小差」 |
| `INIT_REQUIRED` | 跳 `/setup`（系统未初始化） |
| 其他业务码 | 提示 `data.message` 或 `data.msg` |
| 非 JSON 响应 | 提示「响应异常」（反向代理层 兜底 404 HTML） |

### 5.5 loading 管理

- 列表用 `v-loading` 指令包裹表格
- 提交按钮 `:loading="submitting"` 防重复
- 全局 loading 不允许直接操作 DOM，必须通过组件状态控制

## 6. 路由规范

### 6.1 meta 字段

| 字段 | 类型 | 作用 |
| --- | --- | --- |
| `title` | String | 浏览器标题、面包屑、菜单显示名 |
| `group` | String | 侧边栏分组（`workspace` / `customer` / `aiAgent` / `reach` / `community` / `dataAnalysis` / `system` / `knowledge` / `sales` / `analytics`） |
| `icon` | String | Element Plus 图标组件名（如 `UserFilled`、`Promotion`），由 `ElementPlusIconResolver` 自动按需导入 |
| `requiresAuth` | Boolean | 是否需要登录（默认 true） |
| `requiresAdmin` | Boolean | 是否仅 admin 可访问（router.beforeEach 守卫） |
| `public` | Boolean | 公开页（如 `/chat/embed/:channel_ref`） |
| `hideLayout` | Boolean | 隐藏主 Layout（嵌入页用） |

```js
{
  path: 'campaign/list',
  name: 'CampaignList',
  component: () => import('@/views/campaign/List.vue'),
  meta: {
    title: '市场活动',
    group: 'reach',
    icon: 'Promotion',
    requiresAuth: true
  }
}
```

### 6.2 懒加载

- 所有业务页面使用动态 `import()`：`component: () => import('@/views/foo/List.vue')`
- 路由模块文件位于 `src/router/modules/*.js`，每个文件 `export default []` 路由数组
- 新增模块必须在 `src/router/index.js` 的 `moduleNames` 数组中追加
- 路径首段 kebab-case 与 camelCase 模块名不匹配时，在 `pathToModule` 中显式映射

### 6.3 路径约定

- 子路由 `path` 不以 `/` 开头（自动拼接在 `Layout` 下）
- 资源详情类用 `:id`：`campaign/edit/:id`
- 卡片统计类用 `:id`：`douyin-card-stats/:id`
- 兜底重定向用 `redirect`：`{ path: '/oneid', redirect: '/oneid/list' }`

### 6.4 路由分组映射

| group 值 | 侧边栏分组 |
| --- | --- |
| `workspace` | 工作台 |
| `customer` | 客户管理 |
| `aiAgent` | AI 智能体 |
| `knowledge` | 知识库 |
| `reach` | 营销触达 |
| `community` | 社媒运营 |
| `analytics` / `dataAnalysis` | 分析洞察 |
| `system` | 系统管理（仅 admin） |
| `sales` | 销冠相关（异议处理、画像） |

## 7. 样式规范

### 7.1 SCSS 使用

- 全局样式入口：`src/styles/index.scss`（在 `main.js` 引入）
- 变量定义：`src/styles/variables.scss`（颜色、间距、断点、字体）
- 重置样式：`src/styles/reset.scss`
- 组件级样式：`<style scoped lang="scss">`，禁止全局污染

### 7.2 主题变量

- 颜色统一通过 SCSS 变量引用，**禁止**硬编码色值
- 主题色变更应只改 `variables.scss`，不要在每个组件内 `color: #1890ff`

```scss
// src/styles/variables.scss
$primary-color: #409eff;
$success-color: #67c23a;
$warning-color: #e6a23c;
$danger-color: #f56c6c;
$border-color: #dcdfe6;
$text-primary: #303133;
$text-regular: #606266;

$sidebar-width: 240px;
$sidebar-collapsed-width: 64px;
$header-height: 60px;
```

### 7.3 响应式断点

| 断点 | 宽度 | 用途 |
| --- | --- | --- |
| `xs` | < 768px | 手机 |
| `sm` | ≥ 768px | 平板竖屏 |
| `md` | ≥ 992px | 平板横屏 |
| `lg` | ≥ 1200px | 桌面 |
| `xl` | ≥ 1920px | 大屏 |

- 使用 Element Plus 的 `<el-row :gutter="15">` + `<el-col :xs="24" :sm="12" :lg="8">` 网格系统
- 自定义断点用 SCSS `@media` 配合 variables.scss 中的断点变量

### 7.4 禁止事项

- **禁止 inline style**：`<div style="color: red">` 不允许（除动态计算的样式外）
- **禁止全局选择器**：不要在组件内写无 `scoped` 的样式
- **禁止 `!important`**：除非覆盖 Element Plus 内部样式无法避免
- **禁止深度选择器旧语法**：用 `:deep()` 替代 `::v-deep` / `/deep/`

```vue
<!-- ❌ 禁止 -->
<div style="color: red; padding: 10px">...</div>

<style>
.global-class { color: red; }     <!-- 无 scoped -->
</style>

<style scoped>
::v-deep .el-table__row { }        <!-- 旧语法 -->
</style>
```

## 8. 国际化规范

### 8.1 文案必须走 i18n

- 所有面向用户的文案（按钮、提示、表头、菜单、错误提示）必须通过 `t('key')` 或 `$t('key')` 调用
- 硬编码中文字符串仅允许在注释与 console 中
- 临时调试文案可暂时硬编码，但合并到 main 前必须替换

```vue
<!-- ✅ 推荐 -->
<el-button>{{ $t('common.confirm') }}</el-button>
<el-button>{{ t('common.cancel') }}</el-button>

<!-- ❌ 禁止 -->
<el-button>确认</el-button>
<el-button>Cancel</el-button>
```

### 8.2 key 命名

- 顶层命名空间：`menu.*` / `layout.*` / `system.*` / `http.*` / `common.*` / `validation.*`
- 业务文案：`{module}.{field}` 或 `{module}.{action}.{object}`
- 嵌套不超过 3 层

```json
{
  "menu": { "workspace": "工作台", "customer": "客户管理" },
  "layout": { "login": "登录", "logout": "退出" },
  "http": { "requestFailed": "请求失败", "loginExpired": "登录已过期" },
  "common": { "confirm": "确认", "cancel": "取消", "save": "保存" },
  "customer360": { "phone": "手机号", "email": "邮箱" }
}
```

### 8.3 多语言同步

- 修改 `zh.json` 后必须同步更新 `en.json` / `ja.json` / `ar.json`
- 缺失 key 会显示原始 key（`missingWarn: false`），开发期可临时打开警告排查
- RTL 语言（`ar`）通过 `applyDirection(locale)` 切换 `<html dir="rtl">`

### 8.4 构建期预编译

- `vite.config.js` 中 `@intlify/unplugin-vue-i18n` 在构建期将 JSON 编译为运行时函数
- **禁止**在代码中调用 `i18n.global.getLocaleMessage()` 动态编译消息（会触发 CSP `unsafe-eval`）
- 新增语言文件需在 `vite.config.js` 的 `include` 路径下（默认 `src/i18n/locales/**`）

## 9. 安全规范

### 9.1 JWT 存储

- token 存储在 `localStorage.token`
- 通过 `utils/request.js` 请求拦截器自动注入 `Authorization: Bearer {token}`
- **禁止**将 token 写入 cookie 或 URL（除 WebSocket `?token=` 回退场景）
- 退出登录必须调用 `userStore.logout()`，同时 `localStorage.removeItem('token')` + `localStorage.removeItem('user_info')`

### 9.2 XSS 防护

- 渲染用户输入内容必须用 `v-html` 配合 `dompurify` 清洗（package.json 已引入 `dompurify ^3.4.12`）
- 富文本内容（如 SimpleEditor 输出）入库前后端都要做清洗
- **禁止**直接 `v-html="userContent"` 不做清洗

```js
import DOMPurify from 'dompurify'
const safeHtml = DOMPurify.sanitize(rawHtml)
```

### 9.3 CSRF

- axios 默认携带同源 cookie，依赖 `SameSite=Lax` 防护
- 后端如启用 CSRF token，前端需在请求拦截器中读取 `X-CSRF-Token` 并注入

### 9.4 敏感信息脱敏

- 列表展示 token / API Key / 密码必须脱敏（如 `••••abcd`）
- 详情页可在「显示」按钮点击后临时展示明文
- **禁止**将敏感信息写入 console / 日志 / i18n 文案

```vue
<el-table-column label="Bot Token">
  <template #default="{ row }">
    <span>{{ maskToken(row.botToken) }}</span>
    <el-button text @click="row._showToken = !row._showToken">
      {{ row._showToken ? '隐藏' : '显示' }}
    </el-button>
  </template>
</el-table-column>
```

### 9.5 嵌入页安全

- `/chat/embed/:channel_ref` 标记 `meta.public = true`，不进入主 Layout
- 后端校验来源 origin（在 `chatChannel` 配置中维护白名单）
- 嵌入方通过 iframe 加载，子页面内部**禁止**访问 `parent.window`（XSS 风险）

## 10. 性能规范

### 10.1 路由懒加载

- 所有业务页面 `component: () => import('@/views/...')`
- 60+ 路由模块按需下载，首屏只加载 `Layout` + `Profile` + `Notifications`

### 10.2 keep-alive

- 频繁切换的列表页可考虑 `<router-view v-slot="{ Component }"><keep-alive><component :is="Component" /></keep-alive></router-view>`
- **注意**：keep-alive 会缓存组件状态，编辑类页面不要用（避免脏数据）
- 当前 `Layout.vue` 未全局启用 keep-alive，需要时在 `meta.keepAlive = true` 标记并在布局中按需启用

### 10.3 防抖节流

- 搜索框输入：用 `lodash.debounce` 或自定义 `debounce`，建议 300ms
- 窗口 resize、滚动监听：用 `requestAnimationFrame` 或 throttle，避免每帧触发
- WebSocket 心跳：固定 25s 间隔（参考 `utils/agentSocket.js`）

### 10.4 大列表优化

- 数据量 > 1000 行时使用虚拟滚动：`<el-table-v2>`（Element Plus 2.9+）
- 分页大小默认 20，最大不超过 100
- 树形数据懒加载子节点 `:load="loadNode" lazy`

### 10.5 其他

- ECharts 实例在 `onUnmounted` 中 `dispose()`，避免内存泄漏
- WebSocket 连接在 `onUnmounted` 中 `close()`
- `setInterval` / `setTimeout` 必须在 `onUnmounted` 中清理

## 11. 提交规范

### 11.1 commit message

参考 Conventional Commits：

```
<type>(<scope>): <subject>

<body>

<footer>
```

| type | 含义 |
| --- | --- |
| `feat` | 新功能 |
| `fix` | Bug 修复 |
| `docs` | 文档变更 |
| `style` | 代码格式（不影响功能） |
| `refactor` | 重构（既不是 feat 也不是 fix） |
| `perf` | 性能优化 |
| `test` | 测试相关 |
| `chore` | 构建 / 工具 / 依赖 |
| `ci` | CI 配置 |

```
feat(customer360): 新增客户标签批量编辑功能
fix(request): 修复 401 时未清除 user_info 的问题
docs(dev): 补充 ARCHITECTURE.md 架构图
```

### 11.2 ESLint

- 配置文件：`eslint.config.recommended.mjs`（**当前未启用**，待存量迁移完成后启用）
- 启用步骤见该文件顶部注释
- 核心规则：
  - `no-restricted-imports`：禁止 `import request from '@/utils/request'`，必须用 `import { http } from '@/utils/request'`
  - 存量 43 个文件（使用 default 导入 `import request from`）通过 `overrides` 临时放行，新增文件必须用 `{ http }`
  - `no-console`：允许 `console.warn` / `console.error`，警告 `console.log`
  - `vue/multi-word-component-names`：关闭（允许单词组件名）

### 11.3 Prettier

- 仓库未配置独立 `.prettierrc`，遵循 Vite 默认格式
- 建议团队约定：
  - 单引号 `'`（不混用双引号）
  - 行尾无分号（与现有代码一致）
  - 缩进 2 空格
  - 行宽 100
  - 末尾逗号 `es5`

### 11.4 提交前检查

```bash
# 1. 路由 / 菜单一致性
node check_menu.mjs

# 2. 单元测试
npm run test

# 3. 构建
npm run build
```

## 12. 禁止清单

以下行为**严禁**：

| 序号 | 禁止项 | 原因 |
| --- | --- | --- |
| 1 | 在组件中直接 `import axios from 'axios'` 调用 | 绕过拦截器，丢失 JWT / 错误处理 / 多语言 |
| 2 | 在 store 中操作 DOM（`document.querySelector` 等） | 状态层与视图层耦合，难以测试 |
| 3 | `utils/` 文件膨胀（单文件 > 500 行） | 应拆分；`request.js` 当前 318 行已接近上限 |
| 4 | 文件名带版本后缀（`foo_v1.js` / `foo_v2.js`） | 历史版本应通过 git 管理 |
| 5 | 文件名带时间戳后缀（`foo_20250726.js`） | 同上 |
| 6 | 文件名带临时标记（`foo_new.js` / `foo_tmp.js` / `foo_bak.js`） | 用 git stash / branch 管理 |
| 7 | 在 `<script setup>` 中使用 Options API | 必须用 Composition API |
| 8 | 直接修改 store state（`userStore.token = 'xxx'`） | 必须通过 action |
| 9 | inline style（除动态计算样式） | 用 scoped SCSS |
| 10 | 硬编码色值 / 间距（绕过 `variables.scss`） | 难以维护主题 |
| 11 | `console.log` 提交到 main 分支 | 用 `console.warn` / `console.error` |
| 12 | 中文 / 英文硬编码文案（不通过 i18n） | 多语言不可用 |
| 13 | `v-html` 不做 `DOMPurify.sanitize` | XSS 风险 |
| 14 | 在路由 `path` 末尾用驼峰（`/campaignList`） | 应 `/campaign/list` kebab-case |
| 15 | 在 api 文件中写业务逻辑（如计算合计、过滤） | api 文件只做请求，业务逻辑放组件 / store |
| 16 | `setInterval` / `setTimeout` 不在 `onUnmounted` 清理 | 内存泄漏 |
| 17 | ECharts 实例不 `dispose()` | 内存泄漏 |
| 18 | 跨模块直接 import 其他模块的内部组件（如 `views/customer360/components/Foo.vue`） | 应提升到 `src/components/` |
| 19 | 在 `.vue` 文件中写超过 1000 行 | 应拆分子组件 |
| 20 | 修改 `src/types/components.d.ts`（自动生成） | 由 `unplugin-vue-components` 自动生成 |

## 关联文档

- [ARCHITECTURE.md](./ARCHITECTURE.md) - 架构图
- [DEVELOPMENT.md](./DEVELOPMENT.md) - 代码开发手册
- [FEATURES.md](./FEATURES.md) - 功能清单
- [../../MENU_SPEC.md](../ui-inventory/MENU_SPEC.md) - 菜单页面规格清单
- [../../eslint.config.recommended.mjs](../../eslint.config.recommended.mjs) - ESLint 推荐配置

---

最近更新日期: 2026-07-26
