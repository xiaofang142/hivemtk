# AGENTS.md — 所有 AI 编辑器通用入口（本仓最高规则摘要）

> 本文件是**跨编辑器统一入口**：OpenCode / Codex / Cursor / Windsurf / Copilot / Claude 均读它。
> Claude 另外读 `CLAUDE.md`（更详细的版本，本文件与其冲突时以 `CLAUDE.md` 为准）。
> 深度规范见 `docs/standards/AI-EDITOR-CONTRACT.md`（AI 必须逐条遵守的契约）。
> **本文件只放"必须记住的硬规则 + 去哪查"，不放长教程。**

---

## 0. 先定位产品根目录（工作区根不是 git 仓，勿猜层级）

工作区根 `~/Documents/www/go/hivemtk` 是**普通目录、没有 `.git`**；用户端主产品仓是与其平级的
`~/Documents/www/go/hivemtk/hivemtk-user`（**独立 git 仓，非 submodule**）。真实代码
（`deploy/`、`user-server/`、`user-web/`、`skills/`）都在 `hivemtk-user/`。
工作区根也有一个 `deploy/`——那是平台侧另一套发布入口（`deploy.sh` + `deploy.conf`），
**不含判据文件 `deploy-api.sh`**（判据文件只在 `hivemtk-user/deploy/` 存在，2026-11-25 实测）。
`git rev-parse --show-toplevel` 从产品仓子目录跑会返回 `hivemtk-user` 本身，
`--show-superproject-working-tree` 为空；不要按 submodule 或旧的内层 `hivemtk/` 处理。

⇒ 拿“应用根目录”必须**从产品仓目录向上查找判据文件**；工作区根没有 `.git`，
不再使用 `git rev-parse --show-toplevel` 拼接旧内层目录的兜底：

```bash
# 在产品仓任意目录（含 user-server/、docs/）启动时运行：
APP=$PWD
while [ "$APP" != "/" ] && [ ! -f "$APP/deploy/deploy-api.sh" ]; do APP=$(dirname "$APP"); done
[ -f "$APP/deploy/deploy-api.sh" ] || { echo "未找到主代码目录（含 deploy/deploy-api.sh）；若从工作区根启动，请先 cd hivemtk-user"; exit 1; }
# APP = <workspace>/hivemtk-user

# 从工作区根启动时，先进入平级产品仓再执行上面同一段：
cd ~/Documents/www/go/hivemtk/hivemtk-user
```

**新机器/新 clone 第零步（缺了它约束体系静默残缺，2026-11-14 初心审核实测）**：

```bash
bash scripts/bootstrap-agent-env.sh    # 配置 git 钩子 + 安装 skills 到本机编辑器目录 + 自检
```

不做这步：commit-msg 白名单不生效、本机 agent 身上没有任何本仓 skill（编码动作序列
`hivemtk-coding`、发布链 `hivemtk-release`、浏览器规则 `browser-debug-rule` 全部缺失）。

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

### 规则 0b：主智能体编排子智能体（最高优先级）

- 子智能体由**主智能体统一编排**：主智能体负责调度、监督、纠正，子智能体负责执行具体任务。
- 串行或并行由主智能体按任务依赖、资源约束与用户指令决定；本仓不预设禁止并行。
- 用户明确要求“一次一个任务”时，按该纪律逐个派发、回收结论、审查纠正后再派下一个；本次文档审计即遵循该串行纪律。
- 子智能体定义须含清晰任务边界、验收判据与文件路径；本规则优先于其他规则。

### 规则 2：发现问题必须立即修复，禁止记录待办。

## 2. 代码规格（铁律）

### 后端（`user-server/`，Go）

**五层架构：Router → Controller → Service → Repository → Model，禁跨层。**

| 层 | 职责 | 禁止 |
|---|---|---|
| Router | URL→Controller 映射 + **依赖装配**（AV-04c 后的既成装配点，2026-11-14 溯源修正） | 写业务逻辑、内联闭包 |
| Controller | 解析入参、调 Service、组响应 | 直接碰 DB / 调 Repository |
| Service | 业务编排、事务边界 | 写 SQL / 碰 `*gin.Context` 细节 |
| Repository | 数据访问（SQL/GORM） | 写业务规则 |
| Model | 数据结构与表映射 | 带行为逻辑 |

- 代码位置：`user-server/internal/{router,controller,service,repository,model}/`。
  **本仓无 `handler` 包**：新域一律 `XxxController`，不引入 handler 同义词
  （2026-11-25 快照：`internal/controller/` 241 个顶层非测试 `.go`、231 个 `XxxController` struct 类型、`XxxHandler` struct 零命中；计数方法与最新快照见
  `../docs/architecture/FULL_FEATURE_INVENTORY.md` §3 I-04，不在此长期硬编码维护）。
- 分层有 CI 静态门：`python3 scripts/check-layer-boundary.py`（GitHub Actions `Layer Boundary`
  作业强制，新增跨层 import 即红）。存量例外 2026-11-14 已清零：router 层 33 处装配经
  AV-04c 溯源转为合法口径，controller→gorm 与 service→gin 共 5 处已重构消除；
  `scripts/layer-boundary.baseline` 机制保留，目标恒空。
- API 路径规范 `/api/{domain}/{resource}`；响应格式、错误码双通道口径见 `CLAUDE.md` §API 规范。
- 多租户 fail-closed：任何表访问必须带租户上下文，缺上下文一律拒绝而非放行。

### 前端（`user-web/`）

- 管理端：Vue3 + ElementPlus（`user-web/src/`）。
- 扩展端：Chrome MV3（`user-web/bridge/`），Vue3 popup + 多 content script；
  渠道适配器在 `user-web/bridge/src/channels/`，共享能力在 `src/core/`。
- 隐私红线：源码内**不得出现厂商域名字面量**（线上地址经构建期 env 注入）。
- 新功能必须自动纳入三层引导体系（`useOnboarding.ts`）并补 i18n 文案——见 `CLAUDE.md` §规则5。

### 代码区 × 约束覆盖矩阵（初心审核 2026-11-14：目录不止 user-server，每区都有归属）

| 代码区 | 标准文档 | 机器门（CI/本地） | 发布口径 |
|---|---|---|---|
| `user-server/`（Go 后端） | `BACKEND-CODING-STANDARDS.md` | layer-boundary / api-contract / enum-consistency / env-example-gate / seam-guard / user-server-ci / `make audit` | 链 A `deploy/deploy-api.sh` |
| `user-web/src/`（管理端 SPA） | `FRONTEND-CODING-STANDARDS.md` §1 | eslint 门（基线 63/门 80）+ vitest + api-contract | **链 D 已成文（2026-11-19 实测）**：build → tar 备份 → rsync dist → `mtk-prod:/www/wwwroot/hivemtk/user-web/`，nginx 宿主机直伺服，公网验 index 资产 hash（见 RELEASE-PIPELINE §链 D） |
| `user-web/bridge/`（Chrome MV3 扩展） | `FRONTEND-CODING-STANDARDS.md` §2 | vitest（基线 90+ files）+ ci-bridge | 链 B `npm run publish -- --reload` + 链 C 上链 |
| `embed-sdk/`（客服 Widget，ADR-011） | `FRONTEND-CODING-STANDARDS.md` §3 | ci-embed-sdk（unit+boundary 62 用例，2026-11-14 补） | **链 D 已成文（2026-11-19 实测）**：build → rsync dist → `mtk-prod:/www/wwwroot/hivemtk/user-server/embed-sdk-dist/`，nginx `/embed/` 伺服，公网验 `/embed/marketing-chat-widget.iife.js` 200（RELEASE-PIPELINE §链 D） |
| `packages/`（共享 JS：browser-core/http-core） | —（被 user-web `src/utils/request.js` 消费，随其 vitest 门 + ci-embed-sdk 触发） | 同左 | 随消费方 |
| `website/`（官网/术语/工具脚本） | `website/TERMINOLOGY.md`（命名权威） | markdownlint + docs-link-check + `check_i18n.mjs` | — |
| `../sagex/`（SageX Android 客户端，平级独立 git 仓库） | — | Android Gradle（Java 17）+ R8 混淆；授权/激活链路见 `app/src/main/java/.../license/` | 独立仓库，不随链 A/B/C/D 发布 |
| `migrations/`+`user-server/internal/model/` | — | enum-consistency（SQL↔Go 枚举） | 随链 A |
| 外层 `hivemtk-platform/`、`assetdpo/` | **各自仓库自治**（各有自己的 CLAUDE.md/GIT_RULES），本仓约束体系不跨仓强制 | 各仓自带 | 各仓自己的发布入口 |

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
三条链（A/B/C）+ 换机人工步，一条命令一步：

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
| **spec-kit 双车道执行手册**（规则3/4 分流表/命令体/铁律全文） | `docs/standards/SPEC-KIT-WORKFLOW.md` |
| 编码主规则 / i18n 策略 / 文档模板 | `docs/standards/`（索引见其 README） |
| 开发手册（AI 版 / 人类版） | `docs/handbook/` |
| 协作规范 / 代码地图 / Git 规则 | `docs/contribute/`（CODE_COLLABORATION.md / CODEBASE_MAP.md / GIT_RULES.md） |
| 发布链 / 线上拓扑台账 / 排障 | `docs/operations/`、`docs/TROUBLESHOOTING.md` |
| 桥接架构（HTTP 三通道） | `docs/bridge/BRIDGE-ARCHITECTURE-2026-08-14.md` |
| 文档总索引 | `docs/DEV_DOCS_INDEX.md`、`docs/INDEX.md` |
