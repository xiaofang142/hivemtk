# HiveMtk 前端编码规范（管理端 Vue3 + 蜂桥 Chrome MV3 扩展）

> **触发条件**：改动落在 `user-web/**`（管理端或 `user-web/bridge/` 扩展）之前**必读**。
> 只改后端请读 `BACKEND-CODING-STANDARDS.md`；只改接口契约请读 `INTERACTION-STANDARDS.md`。
>
> **权威源**（本文所有规则均有仓内实证，非拍脑袋）：
> - `docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md`（后端五层，⭐⭐⭐）
> - `user-web/eslint.config.mjs`（flat config，CI 在 `.github/workflows/user-server-ci.yml` 的 `user-web-lint` job）
> - `user-web/vitest.config.js`、`user-web/bridge/scripts/release.mjs`
> - `docs/standards/INTERACTION-STANDARDS.md`（接口与 toast/i18n 口径）
> - `docs/operations/RELEASE-PIPELINE.md`（发布链 SOP）
>
> 版本：v1.0 · 2026-11-13

---

## 0. 两个前端，两套约束

| 前端 | 位置 | 技术栈 | 跑什么命令 |
|---|---|---|---|
| **管理端** | `user-web/src/` | Vue 3 + Element Plus + Vite + vue-i18n | `npm run lint:check` / `npm run test` |
| **桥接扩展** | `user-web/bridge/` | Chrome MV3（manifest_version 3），原生 JS content script + Vue3 popup | `npm run lint` / `npm run test` |

**铁律**：改哪个前端跑哪个的门，不要只跑一个就当全绿。

---

## 1. 管理端（Vue 3 + Element Plus）

### 1.1 目录结构（实测）

```
user-web/src/
├── api/          124 个文件，一业务域一文件，只放请求函数
├── views/        255 个文件，页面组件（与 router/modules 对应）
├── router/       index.js + modules/（40+ 业务域模块）
├── constants/    23 个文件，常量与枚举
├── utils/        20 个文件（request.js / tokenRefresh.js 在此）
├── components/   18 个文件，跨页复用组件
├── i18n/         index.js / locale.js / locales/（10 语言）+ TRANSLATOR_GUIDE.md
├── stores/       9 个文件（pinia）
└── layout/ composables/ styles/ config/ types/
```

### 1.2 API 层铁律（`src/api/*.js`）

```js
import { http } from '@/utils/request';

// AM-02：商户积分余额自查（透传平台 MerchantCreditView）
export const listAssets = (params) => http.get('/api/v1/asset-market/list', params);
export const getAsset = (id) => http.get(`/api/v1/local-assets/${id}`);
```

1. **只 named import**：`import { http } from '@/utils/request'`。eslint `no-restricted-imports`
   **禁止 default 导入** `@/utils/request`（存量 43 文件走 overrides 临时放行待迁移 OPT-FE-03，新文件不许再加进豁免）。
2. 路径一律带 `/api/v1/` 前缀；id 用模板字面量拼接，不要字符串加号。
3. 命名语义化：`listXxx` / `getXxx` / `createXxx` / `updateXxx` / `deleteXxx` / `toggleXxx`。
4. **api 文件只放请求函数**，不放业务判断、不放 state、不放格式化逻辑。
5. 行内中文注释写**业务缘由**（为什么要有这个接口/字段），不要复述函数名。

### 1.3 路由（`src/router/`）

- 按业务域拆模块：一域一文件放在 `src/router/modules/`，由 `index.js` 汇总。
- `name` = PascalCase 组件名；`path` = kebab-case。
- **改路由必须同步 `router-list.md` 台账**（仓根一份、`user-web/` 一份同源）——这是 UI 回归的逐页清单。
- 新路由要配新手引导（三层体系，见 `INTERACTION-STANDARDS.md` §5）。

### 1.4 组件规范

- 单文件组件 `<script setup>` + `<template>` + `<style scoped>`。
- `vue/multi-word-component-names: off`（Vue3 script setup 不需要 import Vue），但**仍然要求多词命名**，
  避免与 HTML 原生元素撞名。
- 表格/表单一律用 Element Plus 组件，不要手写裸 table + 裸 input（可访问性与风格统一）。
- 交互必须可访问：eslint 挂了 `vuejs-accessibility`，图标按钮要 `aria-label`。

### 1.5 eslint 规则（CI 阻断，`npm run lint:check`）

| 规则 | 值 | 说明 |
|---|---|---|
| `no-restricted-imports` | 禁 default 导入 `@/utils/request` | 见 §1.2 |
| `no-console` | `['warn', {allow:['warn','error']}]` | 生产禁 console.log |
| `no-unused-vars` | `['warn', {argsIgnorePattern:'^_', varsIgnorePattern:'^_'}]` | 下划线前缀=有意不用 |
| `vue/multi-word-component-names` | `off` | 见 §1.4 |
| warning 门 | **基线 63、门 80** | `npm run lint` 用 `--max-warnings=80`；新增 warning 会吃掉余量，**别留 warning** |

---

## 2. 桥接扩展（Chrome MV3，`user-web/bridge/`）

### 2.1 结构（实测）

```
user-web/bridge/
├── manifest.json          MV3 清单，version 与 package.json 必须一致
├── src/
│   ├── background/        service_worker（index.js / injector.js / sse-client.js）
│   ├── content/           9 个 content script 入口（douyin/xhs/tiktok/bilibili/weibo/xianyu/jd/kuaishou/pdd）
│   ├── channels/          9 个渠道适配器（BaseAdapter 子类，平台 DOM 差异全在这一层）
│   ├── core/              21 个共享模块（channel-adapter / uplink / downlink / rate-limiter / sanitize / dom…）
│   └── popup/             Vue3 设置面板
├── scripts/               build.mjs / release.mjs / publish.mjs / install-extension.mjs / upload-platform-asset.mjs
└── test/                  vitest 用例
```

- `manifest_version: 3`；`background.service_worker: "background.js"`；CSP
  `script-src 'self'; object-src 'self'` ⇒ **禁止 inline script / eval / new Function**。
- content script 按域名精确挂（`https://*.douyin.com/*` 等 9 个），新增渠道必须同时加
  manifest 匹配 + `channels/<name>.js` 适配器 + `content/<name>.js` 入口。

### 2.2 平台适配器的铁律（改 `channels/*.js` 前必读）

渠道适配器是**唯一**知道平台 DOM 长什么样的层。所有平台差异（选择器、消息 id、会话 id、
账号提取）必须关在 `channels/<platform>.js` 里，`core/` 只放平台无关逻辑。

1. **选择器改动必须真机核对**：用 ego-browser 打开真实页面读 DOM 后再改，不许凭记忆/猜。
   已有多轮实证教训：B站哈希类名 `_Msg_*` / `_SessionItem_*`（`.msg-item` 系全不命中）、
   微博 `.name .hidden` 藏 uid、小红书无发送按钮只能合成 Enter。
2. **消息 id**：`core/channel-adapter.js` 的 `_canonicalMsgId` 统一生成；同内容第 n 条加 `#<n>` 后缀。
   不要自造 id 规则。
3. **系统消息必须进漏斗**：平台通知/限流文案/「暂不支持该消息类型」等不能当客户消息上行触发 AI
   （各渠道 `isSystemText` / `isSystemMessage` 词表，`SYSTEM_TEXT_PATTERNS`）。
4. **账号与会话 id 取稳定值**：账号取 SSR/`window.SSR_RENDER_DATA` 等页面真实数据源，
   会话 id 取平台稳定 id（data-id / uid / hash 路由 mid），**不许用昵称派生 `conv:<昵称>`**
   （改昵称/同名会串台，P2-8 实测教训）。
5. **下行必须有风控**：`core/rate-limiter.js` 三层（全局间隔 + 账号桶 + 会话桶），
   新渠道不许绕过它直发。

### 2.3 隐私红线（硬禁止）

**源码零厂商域名字面量**：`douyin.com` / `xiaohongshu.com` 等平台域名只能出现在
`manifest.json` 的 `matches` 与 `src/core/constants.js` 的注释里（作文档源），
业务代码不得硬编码。线上服务地址一律走构建注入 `__BRIDGE_ONLINE_SERVER__`
（`BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn npm run release`），
不带 env 的产物预填 localhost，**禁止把 localhost 包当线上包发**。

### 2.4 发版两道门（`npm run release` 前置，缺一 exit 1）

1. `package.json` 与 `manifest.json` **同时** bump 到同版本
   （历史事故 055fa098：只 bump manifest 到 1.2.1、package 停 1.1.0 ⇒ 发版链实际不可用）。
2. `scripts/release.mjs` 的 `CHANGELOGS` 必须有当前 version 条目。

版本历史：1.0.2 → 1.0.3 → 1.0.4/1.0.5 → 1.2.0 → 1.2.1 → 1.3.0。

---

## 3. 给 AI 编辑器的可判定约束

1. 改 `user-web/src/**` ⇒ 跑 `cd user-web && npm run lint:check && npm run test`。
2. 改 `user-web/bridge/**` ⇒ 跑 `cd user-web/bridge && npm run lint && npm run test`。
3. 新 api 文件必须 named import `{ http }`；**不许** `import request from '@/utils/request'`。
4. 新增路由 ⇒ 同步 `router-list.md`（仓根 + `user-web/` 两份）。
5. 改 `channels/*.js` 选择器 ⇒ 先 ego-browser 真机读 DOM，把证据写进提交信息或注释。
6. 新增渠道 ⇒ manifest matches + channels + content 三处齐，缺一不发。
7. 扩展发版 ⇒ package.json / manifest.json / release.mjs CHANGELOGS 三处同版本。
8. **不许**在源码硬编码平台域名或线上地址（隐私红线）。
9. 安装/重载扩展 ⇒ 走 `npm run publish -- --reload`，**不许**移除重装
   （路径变 ⇒ 扩展 ID 变 ⇒ chrome.storage 全丢 ⇒ 可能重复发送历史消息）。

---

## 4. 深度参考

| 主题 | 文档 |
|---|---|
| 发布链 SOP（构建/安装/上链） | `docs/operations/RELEASE-PIPELINE.md` |
| 接口信封/toast/i18n/引导 | `docs/standards/INTERACTION-STANDARDS.md` |
| 测试规范 | `docs/standards/TESTING-STANDARDS.md` |
| 桥接架构 | `docs/bridge/BRIDGE-ARCHITECTURE-2026-08-14.md`（注意：该目录被 .gitignore 忽略，未入仓） |
| 路由台账 | `router-list.md` / `user-web/router-list.md` |
