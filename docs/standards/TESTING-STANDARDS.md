# HiveMtk 测试规范（Go 后端 / Vue 管理端 / 桥接扩展 / UI 真机）

> **触发条件**：写任何测试、改测试基建（vitest.config / testutil / settle-helper）、
> 判断「某个测试失败是不是我的锅」，**之前必读**。
>
> **权威源**（全部仓内实证）：
> - `scripts/check-architecture.sh`（CI 架构门禁，测试也受 §6 文件命名约束）
> - `user-web/vitest.config.js`、`user-web/bridge/vitest.config.js`
> - `user-web/tests/unit/settle-helper.js`（组件测试等待基建）
> - `user-web/bridge/test/*.test.js`（扩展侧 vitest 用例）
> - AGENTS.md 规则-1（UI 测试逐元素显式操作）
>
> 版本：v1.0 · 2026-11-13

---

## 1. 分层测试地图（改哪层跑哪个）

| 改动 | 必跑命令 | 门禁口径 |
|---|---|---|
| `user-server/**` | `go build ./... && go vet ./... && go test ./internal/...` | 失败即红 |
| `user-web/src/**` | `cd user-web && npm run test && npm run lint:check` | 全量通过 + warning ≤ 80（基线 63） |
| `user-web/bridge/**` | `cd user-web/bridge && npm run test && npm run lint` | 全量通过 + warning ≤ 80 |
| 任意 UI 页面 | ego-browser 逐元素真机点验 | 见 §4 |

---

## 2. Go 后端测试

### 2.1 主流写法（仓内实证：1916 个 `*_test.go`）

- **标准库 `testing` 为主**：用 testify 的仅 56 个、table-driven（`tests := []struct`）102 个。
  ⇒ 新测试**默认标准库**，除非所在包已统一用 testify，跟随局部风格。
- 函数名 `Test<Subject>_<Condition>`，如 `TestAbExperiment_AssignStable`、
  `TestAbExperiment_AssignBalance1000`。
- 断言用 `t.Fatalf`，**错误消息写中文**，说清「期望什么、实际什么」。
- 用例上方写**中文注释**说明这条测什么（如同输入恒同输出且输出∈{control,treatment}、
  1000 样本均衡性落在 45%-55%）。
- service 测试 import `hivemtk-user/internal/pkg/testutil` 基建 + repository stub 替身
  （Service 可 Mock Repo 正是五层架构可测试性的体现）。

### 2.2 DB 门禁测试（重要，别误判）

需要 Postgres 的测试会走安全门，**环境缺 `POSTGRES_TEST_PASSWORD` 时直接 fail-closed**：

```
测试库配置未通过安全门（未发起任何连接）：test password is missing;
set POSTGRES_TEST_PASSWORD or POSTGRES_PASSWORD
```

⇒ 这类失败**不是代码问题**，是环境没配密码。判读方式：
`go test ./... 2>&1 | grep -c "test password is missing"` 与 `^--- FAIL` 计数相等 ⇒ 全部是门禁失败。

### 2.3 与架构门禁的关系

测试文件同样受 `check-architecture.sh` [6/10] 文件命名约束的**豁免**：
含 `Test` / `Benchmark` / `Example` 的真测试文件不受 `_stub`/`_new` 等禁用后缀限制；
但 `[9/10]` 上下文透传规则**明确排除 `_test.go`**（测试替身实现的是第三方接口，不强制 ctx 首参）。

---

## 3. 前端测试（Vue 管理端）

### 3.1 配置口径（`user-web/vitest.config.js`，勿随手改）

| 项 | 值 | 纪律 |
|---|---|---|
| `environment` | `jsdom` | — |
| `globals` | `true` | 直接 `describe/it/expect` |
| `include` | `tests/unit/**/*.{test,spec}.{js,mjs,ts}` | 只跑 unit |
| `exclude` | `node_modules`、`dist`、`tests/e2e/**` | e2e 不走 vitest |
| `fileParallelism` | `true` | 并行恢复**前提**见 §3.2 |
| `testTimeout` | 15000 | **禁放大掩盖真悬挂** |
| `hookTimeout` | 30000 | 同上 |
| `coverage` | provider v8，include 仅白名单目录（`src/utils`、`src/api/{backup,order,securityAudit}.js`、`src/views/{backup,order,securityAudit}/**`） | 扩覆盖要同步白名单 |

`testTimeout`/`hookTimeout` 是 N-21（2026-11-12）为高负载防误杀确定性慢用例调的；
配套纪律是**用确定式等待排除假绿，不许靠放大超时**。

### 3.2 组件测试等待（并行恢复的前提）

所有组件用例必须用 `tests/unit/settle-helper.js` 的 `settle()` / `settleBody()`。
**禁止**裸 `flushPromises()` 与固定次数连刷（`for (i<10) await nextTick()`）——
前者等不到异步链尾，后者是猜次数，两者都是假绿来源，且破坏 `fileParallelism` 的安全性。

### 3.3 用例写法

- 一文件一主题，`describe('<组件/函数>')` + `it('<场景>')`，it 文案写**行为**不写实现。
- 接口层用 `vi.mock` 拦 `@/utils/request`，不要在单测里发真请求。
- 断言用户可见结果（DOM 文本 / toast 调用 / 路由跳转），不断言内部 state。

---

## 4. UI 测试（规则-1，最高优先级）

**任何 UI 测试 / 页面点验 / 回归验证，只允许用 ego-browser 对具体元素发显式操作。**

固定流程：`page.goto` → `page.snapshot()` 读语义快照 → **逐个明确指定**要点的按钮/要填的输入
（`ref=` / `css=` 精确到那一个元素）→ 看快照/文本/网络确认结果 → 下一个元素。

**硬禁止**：
- ❌ 自动枚举 + 循环点击：`document.querySelectorAll` 收集全部可点元素后 for 循环 click。
- ❌ 跑批器脚本：`run*.mjs` / `runall*.py` / `page.py` / `probe*.mjs`（历史已废弃，见到即删）。
- ❌ 定时任务式连续翻页、把一整页交互塞进一个自动循环。
- ✅ 允许的辅助：`page.snapshot()`、精确 `loc/ref` 点击与 `fill/selectOption/press`、
  `page.fetch`/curl 核对接口、构建/测试命令。
- ✅ **用 curl 或 `page.fetch` 调接口校验数据不算模拟点击**。

每完成一个元素/一页必须留**真实证据**：快照文本、接口状态码、toast 文案、截图路径之一。
无证据不得声称「已测/已修」。

### 4.1 浏览器工具链优先级

`curl`（公开内容）→ **ego-browser（主方案，复用登录态）** → browser-harness / chrome-devtools MCP
（仅 ego-browser 故障时回退，CDP 9223 / profile `/Users/xiaofang/chrome-main-profile`）→
playwright MCP（仅跨浏览器/mock/用户明确同意）。

禁止独立 profile、`--isolated`、临时浏览器、要求用户重新登录。

---

## 5. 桥接扩展测试（`user-web/bridge/test/`）

- 同样 vitest，一渠道一文件（`channel-<platform>.test.js`）。
- **fixture 必须按真机 DOM 结构构造**：平台哈希类名（B站 `_Msg_*` / `_SessionItem_*`）、
  微博 `.name .hidden` 藏 uid、小红书卡片形态系统消息等，全部来自 ego-browser 真机读取。
  不许凭记忆写选择器 fixture（多轮实测教训：猜的选择器上线全不命中）。
- 覆盖要点：消息去重、系统消息漏斗、账号/会话 id 稳定性、风控限流、下行 ack 终态。
- 现网 DOM 变更后，先真机核对再改 fixture，并在提交信息里写实证依据。

---

## 6. 给 AI 编辑器的可判定约束

1. 改后端 ⇒ `go build ./...` + `go vet ./...` + `go test ./internal/...` 三项都要过。
2. Go 失败先 `grep "test password is missing"` 判是不是 DB 门禁，别把环境问题当代码 bug。
3. **不许**为让测试过去而放大 `testTimeout` / `hookTimeout`。
4. 组件测试必须用 `settle()` / `settleBody()`，**禁止**裸 `flushPromises` 与固定次数连刷。
5. UI 测试必须逐元素显式操作 + 每步留证据；**禁止**任何形式的跑批器/循环点击。
6. 新测试文件名不许撞 `check-architecture.sh` [6/10] 禁用后缀（`_v1`/`_stub`/`_new`/`_old`/`_bak`/`_copy`/`_2026-*` 等）；
   Go 测试文件以 `_test.go` 结尾。
7. 扩展 fixture 必须引用真机 DOM 实证，不许拍脑袋写选择器。
8. 接口层单测必须 mock，不发真请求。
9. 声称「已测/已修」必须附证据（快照文本/状态码/toast/截图路径之一）。

---

## 7. 深度参考

| 主题 | 文档 |
|---|---|
| 架构门禁 10 组检查全文 | `scripts/check-architecture.sh` |
| 五层架构 | `docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md` |
| 后端编码规范 | `docs/standards/BACKEND-CODING-STANDARDS.md` |
| 前端编码规范 | `docs/standards/FRONTEND-CODING-STANDARDS.md` |
| UI 点验规程 | AGENTS.md 规则-1 |
