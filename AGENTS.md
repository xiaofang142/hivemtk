# AGENTS.md — 所有 AI 编辑器通用入口（本仓最高规则摘要）

> 本文件是**跨编辑器统一入口**：OpenCode / Codex / Cursor / Windsurf / Copilot / Claude 均读它。
> Claude 另外读 `CLAUDE.md`（更详细的版本，本文件与其冲突时以 `CLAUDE.md` 为准）。
> 深度规范见 `docs/standards/AI-EDITOR-CONTRACT.md`（AI 必须逐条遵守的契约）。
> **本文件只放"必须记住的硬规则 + 去哪查"，不放长教程。**

---

## 0. 先定位仓库根目录（本仓是嵌套 git 仓库，勿猜层级）

外层 `~/Documents/www/go/hivemtk` 与内层 `~/Documents/www/go/hivemtk/hivemtk`
**各有一个 `.git`**；真实代码（`deploy/`、`user-server/`、`user-web/`、`skills/`）在**内层**，
外层没有 `deploy/`。`git rev-parse --show-toplevel` 从内层子目录跑会返回内层本身，
但 `--show-superproject-working-tree` 为空（**不是 submodule 关系**，勿按 submodule 处理）。

⇒ 任何脚本/命令要拿"应用根目录"必须**向上查找**，不许用 `show-toplevel` 拼 `$ROOT/hivemtk`：

```bash
APP=$PWD
while [ "$APP" != "/" ] && [ ! -f "$APP/deploy/deploy-api.sh" ]; do APP=$(dirname "$APP"); done
# APP = /Users/xiaofang/Documents/www/go/hivemtk/hivemtk
```

## 1. 最高规则（凌驾一切，违反即失败）

### 规则 -1：UI 测试 / 页面点验 / 拟人点击 —— 只允许逐元素显式操作

- 每页流程固定：`goto` → `snapshot` 读语义快照 → **由人逐个明确指定**要点的按钮/要填的输入
  （`loc=` / `ref=` / `css=` 精确到那一个元素）→ 看快照/文本/网络确认结果 → 下一个元素。
- **严禁**"自动枚举 + 循环点击/批量点击"：禁止遍历全部可点元素后 for 循环 click，
  禁止 `run*.mjs` / `runall*.py` / `page.py` 这类跑批器，禁止定时任务式连续翻页，
  禁止把一整页交互塞进一个自动循环。**哪怕脚本里包了 ego-browser，只要它在替人"自动找元素并点"，就是违规。**
- 禁止复用历史批量脚本（`/tmp/ui-audit/run.mjs`、`run2.mjs`、`run3.mjs`、`runall*.py`、
  `page.py`、`probe*.mjs` 等），见到即删，不得"稍改一下再跑一遍"。
- 允许且必须使用的辅助只有：`snapshot` 读页面、精确 `loc/ref` 点击与 `fill/selectOption/press`、
  `page.fetch/curl` 核对接口、mark 脚本更新清单勾选、构建/测试命令。
  **用 curl 或 node 的 fetch 调接口校验数据不算模拟点击。**
- 每完成一个元素/一页必须留下真实证据：快照文本、接口状态码、toast 文案、截图路径之一；
  **无证据不得声称"已测/已修"**。

### 规则 0：Git 协作模式（优先级最高）

- 仓库整体完成才**一次性** `git commit` + push 到**双远端**（`gitee-upstream` + `upstream`），
  禁止小步中途提交。
- `commit-msg` hook 只允许 Conventional Commits 类型：
  `feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert`（**`release` 被拒，用 `build`**）。
- 只提交自己本轮改动的文件；工作区有并行会话未提交文件时**不要 `git add -A` 一把梭**。

### 规则 0b：禁止子 Agent 并行运行，只允许主智能体线性调用。

### 规则 2：发现问题必须立即修复，禁止记录待办。

## 2. 代码规格（铁律）

### 后端（`user-server/`，Go）

**五层架构：Router → Handler → Service → Repository → Model，禁跨层。**

| 层 | 职责 | 禁止 |
|---|---|---|
| Router | 只注册路由与中间件 | 写业务逻辑 |
| Handler | 解析入参、调 Service、组响应 | 直接碰 DB / 调 Repository |
| Service | 业务编排、事务边界 | 写 SQL / 碰 `*gin.Context` 细节 |
| Repository | 数据访问（SQL/GORM） | 写业务规则 |
| Model | 数据结构与表映射 | 带行为逻辑 |

- 代码位置：`user-server/internal/{router,handler/controller,service,repository,model}/`。
- API 路径规范 `/api/{domain}/{resource}`；响应格式、错误码双通道口径见 `CLAUDE.md` §API 规范。
- 多租户 fail-closed：任何表访问必须带租户上下文，缺上下文一律拒绝而非放行。

### 前端（`user-web/`）

- 管理端：Vue3 + ElementPlus（`user-web/src/`）。
- 扩展端：Chrome MV3（`user-web/bridge/`），Vue3 popup + 多 content script；
  渠道适配器在 `user-web/bridge/src/channels/`，共享能力在 `src/core/`。
- 隐私红线：源码内**不得出现厂商域名字面量**（线上地址经构建期 env 注入）。
- 新功能必须自动纳入三层引导体系（`useOnboarding.ts`）并补 i18n 文案——见 `CLAUDE.md` §规则5。

## 3. 调试与验证口径（每条命令都实跑过，照抄即可）

```bash
# 后端
cd user-server && go build ./... && go vet ./...
cd user-server && go test ./internal/service/ ./internal/bridge/... ./internal/channelgw/...
#   ⚠️ DB 门禁测试报 "test password is missing; set POSTGRES_TEST_PASSWORD" = 环境缺变量，
#      fail-closed 未发起任何连接，**不是代码问题**，不得据此判定测试失败。

# 前端扩展
cd user-web/bridge && npx vitest run          # 基线 90+ files 全绿才算无回归
cd user-web && npx eslint src --max-warnings 80   # 全量 warning 基线 63、门 80

# 浏览器类验证：先加载 skill `browser-debug-rule`，主方案 ego-browser（复用登录态）
```

**宣称"已完成/已修复/已通过"前必须贴证据**（测试输出、快照文本、接口状态码、截图路径）。

## 4. 发布（改完了怎么到用户手里）

完整 SOP 见 `docs/operations/RELEASE-PIPELINE.md` §0.1，也可直接加载 Skill `hivemtk-release`。
四条链路，一条命令一步：

| 改动范围 | 命令 |
|---|---|
| `user-server/**`（Go API） | `deploy/deploy-api.sh`（变更驱动 + 双健康检查 + 自动回滚） |
| `user-web/bridge/**`（扩展） | `BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn npm run publish -- --reload` |
| 上链平台（租户下载源） | `node scripts/upload-platform-asset.mjs` |
| 换机器/新加载目录 | 唯一需人工一次：`chrome://extensions` → 开发者模式 → 加载未打包的扩展程序 → 选 `/Users/xiaofang/Documents/tool/hivebridge-1.0.2` |

- 扩展发版两道门：`package.json` 与 `manifest.json` **同时** bump + `scripts/release.mjs` 的
  `CHANGELOGS` 补当前版本条目，否则 `npm run release` 直接 exit 1。
- **保留扩展加载目录路径是硬约束**：路径变 ⇒ 扩展 ID 变 ⇒ `chrome.storage` 全丢
  ⇒ user-server 地址配置与已发送缓存丢失 ⇒ 可能重复发送历史消息。绝不能移除重装。

## 5. 禁止事项（踩过的坑，勿重犯）

- 不得臆测线上版本：后端无公开版本接口，线上版本只以
  `docs/operations/PROD_TOPOLOGY_CHANGES.md` / `deploy/.api-deploy-state.json` 台账为准。
- 不得手工复算平台商户签名 HMAC（本地复算与平台期望不一致，已两次失败）；
  统一走 user-server 代理端点或容器内调用。
- 平台后台「上传扩展包」按钮本身是坏的（`e.upload is not a function`），只能直调 admin 接口。
- ego-browser 子进程 `console.log` 透传父终端、stdout 捕获为空 ⇒ 状态必须写文件回传；
  `nodejs` 不接受脚本文件路径参数，必须 heredoc 或 `-e`。
- 不得要求用户重新登录；不得新建独立 profile / 清 cookie；不得擅自改 ego lite/CDP 配置。
- 不得在未获同意时执行：退出登录、清除站点数据、删除文件、发送消息、购买、关闭未保存窗口。

## 6. 去哪查（深度文档索引）

| 想查什么 | 去哪 |
|---|---|
| 完整最高规则（含 spec-kit SDD 流程） | `CLAUDE.md` |
| AI 必须逐条遵守的契约 | `docs/standards/AI-EDITOR-CONTRACT.md` |
| **后端编码规范**（改 `user-server/**` 前必读） | `docs/standards/BACKEND-CODING-STANDARDS.md` |
| **前端编码规范**（改 `user-web/**` 前必读） | `docs/standards/FRONTEND-CODING-STANDARDS.md` |
| **交互规范**（改接口/toast/i18n/路由前必读） | `docs/standards/INTERACTION-STANDARDS.md` |
| **测试规范**（写测试/判读失败前必读） | `docs/standards/TESTING-STANDARDS.md` |
| 编码主规则 / i18n 策略 / 文档模板 | `docs/standards/`（索引见其 README） |
| 开发手册（AI 版 / 人类版） | `docs/handbook/` |
| 协作规范 / 代码地图 / Git 规则 | `docs/contribute/`（CODE_COLLABORATION.md / CODEBASE_MAP.md / GIT_RULES.md） |
| 发布链 / 线上拓扑台账 / 排障 | `docs/operations/`、`docs/TROUBLESHOOTING.md` |
| 桥接架构（HTTP 三通道） | `docs/bridge/BRIDGE-ARCHITECTURE-2026-08-14.md` |
| 文档总索引 | `docs/DEV_DOCS_INDEX.md`、`docs/INDEX.md` |
