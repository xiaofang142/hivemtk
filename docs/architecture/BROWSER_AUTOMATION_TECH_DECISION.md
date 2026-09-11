# 浏览器自动化模块 — 技术方案论证与决策

> 文档版本：v1.0 ｜ 2026-09-11 ｜ 项目：hivemtk ｜ 性质：技术决策归档
>
> 前置阅读：
> - `user-web/docs/BROWSER_AUTOMATION_TECH_DESIGN.md` — 完整技术设计方案
> - `user-web/docs/BROWSER_AUTOMATION_PROJECT_LANDING.md` — 项目落地设计
> - `user-web/docs/BROWSER_AUTOMATION_DEEP_RESEARCH.md` — Go NM Host 深度调研

---

## 0. 执行摘要（1 分钟读完）

### 0.1 决策

**维持当前选型：Chrome Native Messaging + Go NM Host + Chrome 扩展 + user-server 统一端口架构**

不做方案变更。当前方案经过 2026 年 Chrome 136/147 安全迭代验证，是本项目约束集下的**唯一可行解**。

### 0.2 一页纸

| 维度 | 结论 |
|------|------|
| **为什么不换** | 其他五个方案（Playwright/Puppeteer/Selenium/CDP 直连/Vision）均至少违反一条硬约束 |
| **Chrome 136/147 安全变更影响** | ✅ 零影响——我们不走 `--remote-debugging-port`，Native Messaging 不受 CDP 安全策略约束 |
| **Playwright 退潮趋势** | Browser-Use/Stagehand 放弃 Playwright 是因为「管理浏览器进程 + 新 profile」与我们场景完全不同，我们本来就不用 Playwright 的 |
| **架构改进点** | 6 项：Token 轮换/扩展原语覆盖/健康自检/断线自愈增强/审计日志完善/多 Chrome 通道预留 |
| **演进路线** | v1 稳定化（当前）→ v2 Vision 混合（截图+LLM 视觉兜底）→ v3 多扩展并行 |

### 0.3 关键发现

1. **Chrome 136+（2025 Q1）和 147+（2026）的 CDP 安全硬伤对我们零影响**——这是维持 Native Messaging 路线的最强论据。2025 年起 CDP 远程调试在默认 profile 上被静默拦截，社区涌现大量 hack（profile 复制、弹窗自动点击器、CHROME_CONFIG_HOME 伪造），但都不如 Native Messaging 干净。

2. **Playwright 退潮不意味着「放弃 CDP」**——Browser-Use 和 Stagehand 放弃的是 Playwright 这一层 **Node 进程中继**，转向**直连 CDP**。我们 Go NM Host → Chrome 扩展 → CDP 的链路已经是最轻量的形态了。

3. **业界 Agent 框架（Browser-Use/Stagehand/Skyvern）与我们不在同一层次**——它们是 L3 层的「AI 浏览器代理框架」，我们的 Brain 层 + Executor 层 + 平台适配层 已经覆盖了相似能力，但架构更轻量（无额外 Python/Node 依赖）且与 Go 后端天然融合。

---

## 1. 项目硬约束（不可妥协）

> 源自项目 memory 和既有设计文档的约束集，是方案选型的一票否决项。

| # | 约束 | 为什么不可妥协 | 违反后果 |
|---|------|---------------|---------|
| C1 | **复用用户主 Chrome Profile** | 私域部署场景，企业 SSO/证书/登录态必须继承 | 每次执行都要重新登录，完全不可用 |
| C2 | **后台 tab 不抢焦点** | 不打断用户正常浏览，AI 在你正在使用的浏览器里工作 | 干扰日常工作，产品体验不可接受 |
| C3 | **Chrome 重启后自动连接，无弹窗** | 企业 IT 管理场景，不能依赖人工干预 | 运维复杂度爆炸，无法自动化 |
| C4 | **多 Agent 共享同一浏览器会话** | 自研 Agent + MCP + 第三方 Agent 需共用一个 Chrome | 会话互斥，产品能力受限 |
| C5 | **不引入独立浏览器实例（不是无头、不搞隔离）** | 寄生式体验，接近 ChatGPT Chrome 扩展 | 与 C1 冲突，且额外资源消耗 |
| C6 | **单统一端口架构（:8204）** | user-server 所有服务挂接一个 gin.Engine | 新增端口增加运维和安全面 |
| C7 | **Go 后端为主技术栈** | 项目语言栈决策，不引入 Node/Python 重度依赖 | 增加技术债务和运维复杂度 |
| C8 | **私域单租户部署** | 单一企业独占实例，不搞 SaaS 多租户 | 架构过度设计 |

---

## 2. 业界方案全景（L1→L5 分层 + 2025-2026 最新趋势）

### 2.1 协议栈五层模型

业界浏览器自动化方案可以按**协议栈高度**分成五层，L5 是最抽象的（看像素），L1 是最底层的（Chrome 内核调试协议）：

```
┌────────────────────────────────────────────────────────────────────┐
│ L5  Computer Use (Anthropic)                                       │
│     屏幕截图 + 鼠标键盘事件 — 放弃 DOM，完全视觉化                  │
│     任何桌面应用通吃，但慢（2-5s/步）、贵                           │
├────────────────────────────────────────────────────────────────────┤
│ L4  Chrome 扩展 + Native Messaging  ← 本项目当前选型                │
│     Chrome 扩展（JS）通过 connectNative 连 Native Host              │
│     复用真实 Profile、后台 tab、免弹窗、用户态会话                   │
│     Claude in Chrome（Anthropic 闭源）、chrome-mcp 生态              │
├────────────────────────────────────────────────────────────────────┤
│ L3  AI Agent 框架                                                  │
│     Playwright MCP / browser-use / Stagehand / Skyvern              │
│     新开隔离浏览器实例，新 profile                                   │
│     78K+ stars（browser-use），但无法复用真实登录态                  │
├────────────────────────────────────────────────────────────────────┤
│ L2  浏览器自动化库                                                  │
│     Playwright（Microsoft）/ Puppeteer（Google）/ Selenium          │
│     完整 API 体系、网络拦截、多浏览器引擎                            │
│     2025-2026 行业退潮，Agent 框架纷纷放弃                           │
├────────────────────────────────────────────────────────────────────┤
│ L1  Chrome DevTools Protocol                                        │
│     JSON-RPC 2.0 over WebSocket                                    │
│     Chrome 内核原生调试协议、30+ Domain                             │
│     所有高层方案的基石                                               │
└────────────────────────────────────────────────────────────────────┘
```

### 2.2 2025-2026 关键行业趋势

#### 趋势 1：Playwright 退潮 — 三大框架独立放弃

| 框架 | 时间 | 原因 | 替代 |
|------|------|------|------|
| **Browser-Use** | 2025.08 | Playwright 在 Python↔Node 间制造 double-RPC hop；WS 消息量 326KB vs 直连 11KB（30× 冗余）；fullPage 截图崩在 16000px+ | 自建 `cdp-use`（223 stars），直接 TCP 连 CDP |
| **Stagehand v3** | 2025.10 | 测试框架带来的长尾巴 API 不需要；长时 session 内存增长；iframe/shadow-root 绕 Playwright 层反而多一跳 | 直连 CDP，iframe 交互提速 **44%** |
| **Vercel agent-browser** | 2026 | 保留 Playwright 内部但 Rust CLI + Unix socket 通信 | 上下文消耗降低 93% |

**本质**：Playwright 是为**测试自动化**设计的。Agent 需要**更薄、更直接**的连接。但这与「能否复用真实 Chrome profile」是两个独立问题——Playwright 也能连已有 Chrome（`chromium.connectOverCDP()`），但需要先把 Chrome 以 remote-debugging-port 模式启动，而这恰好被 Chrome 136+ 封杀了。

#### 趋势 2：Chrome CDP 安全收紧 — 从 136 到 147

| Chrome 版本 | 时间 | 安全变更 | 影响 |
|-------------|------|---------|------|
| **136** | 2025.03 | `--remote-debugging-port` 在**默认** profile 上被静默拒绝，必须加 `--user-data-dir` | 所有 CDP 路线必须新开 profile，登录态继承不可能 |
| **147** | 2026 | `IsUsingDefaultDataDirectory()` 硬检查，不弹 Allow 就直接 drop 端口 | browser-harness 等社区涌现 profile-copy / CHROME_CONFIG_HOME hack |
| **148+** | 进行中 | 进一步收紧 nativeMessaging origins 校验 | — |

**社区 workaround 一览**（证明这条路越来越难走）：
- `chrome-cdp-consent-guard` — macOS LaunchAgent 自动点「允许远程调试」弹窗
- browser-harness — Linux 用 `CHROME_CONFIG_HOME` 伪造默认路径；macOS/Windows 复制整个 profile（~2GB）
- MankhongGarden 的 Windows Survival Guide — 9 个 gotchas
- CDP `redaction` — 密码字段在 DOM 中被 Chrome 自动脱敏，某些输入操作失败

#### 趋势 3：Agent 框架走向分层专业化

2026 年 9 月的格局：
- **Browser-Use**（Python, 78K stars, WebVoyager 89.1%）— 自主 Agent 循环，cdp-use 直连
- **Stagehand v3**（TS, 20K stars, Browserbase $300M valuation）— 四原语 act/extract/observe/agent + 自愈 + 动作缓存
- **Skyvern**（Python, Vision + DOM 混合）— 生产级表单填写，85.8% WebVoyager
- **Playwright MCP**（TS, 36K stars, Microsoft 官方）— 确定性测试，零 LLM 开销
- **Claude in Chrome**（Anthropic 闭源）— L4 Native Messaging 路线，实际生产但闭源

### 2.3 核心洞察

> **Playwright 退潮 ≠ 放弃 CDP。** 大家放弃的是 Playwright 这一层 **Node 进程中继**，转向 **直连 CDP**。而我们的链路 Go NM Host → Chrome 扩展 → (CDP) 已经是最轻量的形态了——Chrome 扩展直接跑在 Chrome 进程内，通过 CDP API 操作浏览器，没有任何额外中继层。

---

## 3. 六方案深度对比

### 3.1 方案一览

| # | 方案 | 通信方式 | 浏览器实例 | Profile | 焦点抢占 | 弹窗 |
|---|------|---------|-----------|---------|---------|------|
| S1 | **Chrome Native Messaging**（当前） | stdio 4B+JSON | 复用已有 | ✅ 主 Profile | ❌ 不抢 | ✅ 无 |
| S2 | Playwright（新开） | CDP over WS | 独立 Chromium | ❌ 新 Profile | ✅ 可能 | ❌ 无 |
| S3 | Playwright（connectOverCDP） | CDP over WS | 连已有 Chrome | ⚠️ 必须 `--remote-debugging-port` | ✅ 可能 | ❌ Chrome 弹 Allow |
| S4 | Puppeteer | CDP over WS | 独立 Chromium / connect | 同 Playwright | ✅ 可能 | 同 Playwright |
| S5 | Selenium / WebDriver BiDi | HTTP / CDP | 独立 / connect | 同 Playwright | ✅ 可能 | 同 Playwright |
| S6 | Vision（Computer Use / Skyvern） | 屏幕截图+键鼠 | 任意 | 任意 | ❌ 视觉层 | ⚠️ 取决于底层 |

### 3.2 各方案逐一剖析

#### S1 — Chrome Native Messaging（当前方案）

**架构**：
```
┌─────────────────────────────────────┐
│ Go NM Host (cmd/nm-host/)           │
│  ┌─────────────┐  ┌──────────────┐  │
│  │ stdio pump  │  │ WS → user-srv │  │
│  └──────┬──────┘  └──────┬───────┘  │
│         │ 4B+JSON        │         │
└─────────┼────────────────┼─────────┘
          │                │
┌─────────▼────────────────▼─────────┐
│ Chrome 扩展 (user-web/browser_     │
│              automation/)           │
│  ┌────────────┐  ┌──────────────┐  │
│  │ connectNative│  │ CDP API 调用  │  │
│  └────────────┘  └──────┬───────┘  │
│         │                │         │
│  ┌──────▼────────────────▼───────┐  │
│  │ Chrome 主进程（用户日常 Profile）│  │
│  │ tab_A 日常浏览  tab_B 自动化   │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
```

| 评估项 | 状态 | 说明 |
|--------|------|------|
| 约束 C1 主 Profile | ✅ 满足 | 扩展跑在主 Chrome 里 |
| 约束 C2 后台 tab | ✅ 满足 | `chrome.tabs.create({active: false})` |
| 约束 C3 无弹窗 | ✅ 满足 | Native Messaging 是 Chrome 原生安全通道 |
| 约束 C4 多 Agent | ✅ 满足 | 扩展内 queue 化命令 |
| 约束 C6 统一端口 | ✅ 满足 | Host 连 user-server WS，无额外端口 |
| Chrome 136/147 安全变更 | ✅ 零影响 | 不走 remote-debugging-port |
| 跨平台 | ✅ macOS/Windows/Linux | Native Messaging 全平台支持 |
| 扩展生态 | ⚠️ 需自建 | 无现成 Agent 框架直接复用 |
| 社区成熟度 | ✅ 高 | Chrome 官方稳定 API，manifest v1-v3 都支持 |

**优势总结**：
- 唯一能同时满足 C1-C5 的方案
- 不受 Chrome CDP 安全收紧影响
- Go/Native Messaging/CDP 链路最轻量（无 Node/Python 中继）
- 与项目 Go 技术栈天然融合

**劣势与风险**：
- 用户需要安装 Chrome 扩展 + Go NM Host（install.sh 已自动化）
- 扩展原语覆盖有限——当前 13 种，对比 Playwright 100+ API 仍有差距
- MV3 service worker 30s 超时——**已验证不是问题**（Native Messaging port 天然保活 SW）

#### S2 — Playwright（新开 Chromium 实例）

**架构**：
```
┌──────────────────────────────────────┐
│ Go 后端 ──exec──▶ Node.js Playwright  │
│                         │             │
│                    launch 独立 Chromium│
│                    (全新 Profile)      │
│                    (9222 CDP port)    │
└──────────────────────────────────────┘
```

| 评估项 | 状态 | 说明 |
|--------|------|------|
| 约束 C1 主 Profile | ❌ **违反** | 新开 Chromium，全新登录态 |
| 约束 C2 后台 tab | ✅ 可做到 | Playwright 不抢主浏览器焦点 |
| 约束 C3 无弹窗 | ✅ 无弹窗 | 独立实例不涉及用户浏览器安全策略 |
| 约束 C5 不搞隔离 | ❌ **违反** | 启动独立 Chromium 进程 |
| 约束 C7 Go 技术栈 | ❌ **违反** | 需要 Node.js runtime |

**适用场景**：CI/CD 测试、爬虫集群、一次性自动化任务。**不适合本项目**。

#### S3 — Playwright（connectOverCDP 连已有 Chrome）

**架构**：
```
┌──────────────────────────────────────────┐
│ Chrome 主进程（--remote-debugging-port=9222│
│              --user-data-dir=/dev/null）  │
│                     ▲                    │
│ Node.js Playwright  │ CDP WebSocket      │
│ .connectOverCDP()  │                    │
└────────────────────┴────────────────────┘
```

| 评估项 | 状态 | 说明 |
|--------|------|------|
| 约束 C1 主 Profile | ❌ **Chrome 136+ 禁止** | 必须加 `--user-data-dir` 隔离 |
| 约束 C3 无弹窗 | ❌ **Chrome 136+ 每次弹 Allow** | 用户每次打开 Chrome 都要点 |
| 约束 C7 Go 技术栈 | ❌ **违反** | 需要 Node.js runtime |

**Chrome 136+ 后这条路实际上被官方封杀。** 社区涌现 `chrome-cdp-consent-guard` 等弹窗自动点击器，但本质是 hack。**不适合本项目**。

#### S4 — Puppeteer

与 Playwright 同属 L2 层，由 Google Chrome 团队维护。**和 S2/S3 遇到完全相同的问题**：无法复用主 Profile、CDP 弹窗、需要 Node.js runtime。

唯一差异是 Puppeteer 原生只支持 Chromium，Playwright 多引擎。**不适合本项目**。

#### S5 — Selenium / WebDriver BiDi

Selenium 3.x 走 WebDriver HTTP 协议，4.x 支持 WebDriver BiDi（底层也是 CDP）。**与 S2/S3 同属一类**，优势是跨浏览器（Chrome/Firefox/Safari）但本项目目标明确就是 Chrome。Selenium 社区已萎缩，Playwright/Puppeteer 事实上取代了它。**不适合本项目**。

#### S6 — Vision 方案（Computer Use / Skyvern）

**架构**：
```
┌──────────────────────────────┐
│ 屏幕截图 ──▶ Vision LLM ──▶  │
│  理解 UI  ──▶ 键鼠事件注入    │
│         (macOS Accessibility) │
└──────────────────────────────┘
```

| 评估项 | 状态 | 说明 |
|--------|------|------|
| 约束 C1 主 Profile | ✅ 满足 | 不关心浏览器，看像素 |
| 约束 C2 后台 tab | ❌ **违反** | 需要目标窗在前台（键鼠注入依赖可访问性） |
| 约束 C4 多 Agent | ❌ 困难 | 屏幕是全局资源，多 Agent 争用 |
| 延迟 | ❌ 慢 | 2-5s/步，比 DOM 驱动慢 10×+ |
| 成本 | ❌ 贵 | 每步需要 Vision LLM 推理 |
| 依赖 | ❌ macOS Accessibility | 需要额外授予辅助功能权限 |

**适用场景**：CDP/扩展无法到达的桌面应用、Flash 游戏、Canvas 密集页面。**本项目暂不考虑**，但可作为 v2 兜底方案（当 DOM 定位失败时 fallback 到 Vision）。

### 3.3 决策矩阵

| 约束 \ 方案 | S1 Native Msg | S2 Playwright 新开 | S3 Playwright CDP | S4 Puppeteer | S5 Selenium | S6 Vision |
|-------------|:---:|:---:|:---:|:---:|:---:|:---:|
| C1 主 Profile | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
| C2 后台 tab | ✅ | ✅ | ⚠️ | ⚠️ | ⚠️ | ❌ |
| C3 无弹窗 | ✅ | ✅ | ❌ | ❌ | ❌ | ⚠️ |
| C4 多 Agent | ✅ | ❌ | ⚠️ | ⚠️ | ⚠️ | ❌ |
| C5 不隔离浏览器 | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
| C6 统一端口 | ✅ | ❌ | ❌ | ❌ | ❌ | ⚠️ |
| C7 Go 技术栈 | ✅ | ❌ | ❌ | ❌ | ❌ | ⚠️ |
| C8 单租户 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **通过数** | **8/8** | **3/8** | **1/8** | **1/8** | **1/8** | **3/8** |

> **结论**：S1（当前 Native Messaging 方案）是唯一能同时满足所有约束的方案。

---

## 4. 当前实现评审

### 4.1 已有实现清单

```
user-server/internal/browser_automation/
├── controller/    5 个（task/session/cron/host/platform）
├── service/       10 个（task/session/executor/hand/brain/host_registry/
│                  host_token/feedback/cron/brain_*）
├── repository/     6 个（task/session/step/cron/llm_plan/command_log）
├── model/          6 个（task/session/step/cron/llm_plan/command_log）
├── dto/            3 个（task/session/cron）
└── platform/       3 平台适配器（xiaohongshu/douyin/xianyu）

user-server/cmd/nm-host/
├── main.go                      Go NM Host（stdio pump + WS client）
├── install.sh                   安装脚本（manifest 注册 + token 配置）
└── manifest.json.template       Chrome Native Messaging manifest

user-web/bridge/                  Chrome 扩展（注意：扩展目录名是 bridge）
├── src/background/              Service Worker（连接管理）
├── src/core/                    核心模块（uplink/downlink/dom/...）
├── src/content/                 各平台 content script
├── popup/                       状态面板
└── manifest.json                MV3 manifest
```

### 4.2 做得好的地方

| # | 亮点 | 证据 |
|---|------|------|
| 1 | **完整的 5 层架构** | Router → Controller → Service → Repository → Model，46 个 Go 文件严格分层 |
| 2 | **Brain 模式对标 browser-use** | reflect 状态机（thinking/evaluation/memory/next_goal）+ 循环检测 nudge + 独立 judge 验收 |
| 3 | **Hand 原语丰富** | 16 种原语：open_tab/click/type/snapshot/markdown/screenshot/wait/scroll/extract/post_comment/assert/query/click_near/wait_for_selector/close_tab/tab_exists |
| 4 | **Host Registry 设计正确** | per-user 连接、pending map + req_id 关联、断线自动清理 running session |
| 5 | **平台 L3 适配器模式** | init() 注册、Error 四分类协议（refresh_token/bad_body/retry/disconnect）、契约默认失败（fails-loudly） |
| 6 | **Brain 可靠性设计** | Token 预算熔断、连续失败上限、deadline 看门狗、LLM 幻觉参数钳位 |
| 7 | **HumanizedDelay + 风控检测** | 步间 ±30% 抖动模拟真人、拦截页自动检测 + disconnect 类终止 |
| 8 | **统一端口架构** | Go NM Host 作为 WS client 连统一端口 :8204，不新增任何 HTTP server |

### 4.3 需要改进的地方（6 项）

| # | 改进项 | 当前状态 | 目标 | 优先级 |
|---|--------|---------|------|--------|
| I1 | **扩展原语覆盖率** | Go Hand 16 种，扩展侧需要全部实现 CDP 调用 | 扩展实现完整 16 种 + CDP trusted 事件 | P0 |
| I2 | **Token 轮换机制** | Host Token 静态配置在 `~/.hivemtk/nm_host.conf` | JWT 风格的短时 Token + 后端定时轮换 | P1 |
| I3 | **健康自检脚本** | install.sh 有基础校验 | `scripts/check_browser_host.sh` 一键诊断扩展→Host→user-server 全链路 | P1 |
| I4 | **断线自愈增强** | 目前 Host 断开后 session 置 failed | Host 重连后自动恢复被中断的 session（cron/loop 类） | P2 |
| I5 | **审计日志完善** | BrowserCommandLog 已落库 | 追加扩展侧 console.error/拦截页截图到审计流 | P2 |
| I6 | **多 Chrome 通道预留** | HostRegistry 一对一 userID→HostConn | 预留 userID→[]HostConn 数组，为未来多 Chrome 窗口并行做准备 | P3 |

---

## 5. 风险矩阵

### 5.1 技术风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| Chrome Native Messaging 被废弃 | **极低** | 致命 | Chrome 官方稳定 API，企业扩展广泛依赖；即便废弃我们有 S6 Vision 兜底 |
| MV3 service worker 超时 | **零** | — | Native Messaging port 天然保活 SW（已验证） |
| Go NM Host 跨平台问题 | 中 | 中 | Linux/macOS/Windows 三平台 install.sh + CI 冒烟 |
| 扩展被误删/禁用 | 中 | 高 | install.sh 强校验 host/status API + popup 健康检查 |
| CDP 原语被 Chrome 变更 | 中 | 中 | 原语封装在扩展侧，Chrome API 变更只改扩展不改后端 |

### 5.2 运维风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| 用户不会装扩展/Host | 高 | 高 | install.sh 一键脚本 + README 图文 + popup 状态面板 |
| 扩展版本与 Host 不匹配 | 中 | 中 | hostVersion 字段 + host/status API 版本校验 |
| Host Token 泄露 | 低 | 高 | install.sh 权限 0600 + 后端 `/host/token/reset` API + Token 轮换机制（I2） |
| user-server 重启后 Host 断连 | 中 | 中 | Host 端指数退避重连（2s→60s）+ cronSvc.RestoreAll |

### 5.3 平台演进风险

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| 平台 DOM 变更导致选择器失效 | 高 | 中 | 平台适配器集中管理 + Brain 模式兜底 + Vision v2 预留 |
| 平台风控升级（验证码/滑块） | 中 | 高 | ErrDisconnect 自动终止 + 人工介入标记 |
| Playwright/Puppeteer 有我们要的新能力 | 低 | 低 | 我们走 L4 层，L2 层新能力可以选择性吸收到扩展侧 |

---

## 6. 演进路线图

### v1 — 稳定化（当前，2026 Q3）

```
✅ 核心架构稳定
✅ 5 层架构完整 46 个文件
✅ Brain 模式 reflect + judge + 循环检测
✅ 3 平台适配器（xiaohongshu/douyin/xianyu）
⬜ I1 扩展原语覆盖率审计
⬜ I2 Token 轮换
⬜ I3 健康自检脚本
```

### v2 — Vision 混合（2026 Q4）

```
目标：当 DOM 定位失败时，自动降级到 Vision 方案

┌─────────────────────────────────────┐
│ Executor.dispatchStep() 新逻辑       │
│                                     │
│ 1. 先 DOM 定位（现有扩展原语）       │
│ 2. 失败 → 截图 + Vision LLM 理解    │
│     → 生成 fallback 定位器          │
│ 3. 重试 → 仍失败 → 终止             │
└─────────────────────────────────────┘

增量：
- extension/screenshot_with_ocr 原语
- Brain 层 Vision fallback 模块
- 桌面端 macOS Accessibility API 封装（go:accessibility）
```

### v3 — 多扩展并行 + MCP 开放（2027 Q1）

```
目标：多 Agent 共享 + 跨 Chrome 窗口并行 + 外部 Agent 接入

┌─────────────────────────────────────┐
│ HostRegistry: userID → []HostConn   │
│ （从一对一升级为一对多）              │
│                                     │
│ MCP Server 开放 browser_* tools      │
│ 供 Claude Code / Codex / 自研 Agent  │
│ 通过 MCP over WS 接入               │
└─────────────────────────────────────┘

增量：
- I6 多 Host 通道支持
- internal/aiagent/mcp/ 注册 browser_*
- 扩展多实例（不同 Chrome profile）
```

---

## 7. 与业界对标

### 7.1 vs Browser-Use

| 维度 | Browser-Use | 本项目 |
|------|------------|--------|
| 语言 | Python | Go |
| 浏览器 | 新开 Chromium（cdp-use 直连） | 复用主 Chrome（扩展+NM） |
| Profile | ❌ 新 Profile | ✅ 主 Profile |
| 后台 tab | ❌ 新开浏览器无所谓 | ✅ 不抢焦点 |
| 原语层 | BrowserUseTool（playwright 封装） | 自定义 16 种 |
| Brain 层 | 自研 AgentLoop（89.1% WebVoyager） | BrainService reflect+judge |
| 多平台 | 通用 DOM 适配 | L3 平台适配器（小红书/抖音/闲鱼） |
| 运维复杂度 | Python venv + Node Playwright + Chromium | Go 二进制 + Chrome 扩展 |
| 审计 | 较粗 | command_log + session.step 细粒度 |

**对标结论**：Browser-Use 适合「给我一个新浏览器帮我做完」场景，本项目适合「在你正在用的浏览器里帮你做完」场景。两个产品定位完全不同。

### 7.2 vs Claude in Chrome（Anthropic 闭源）

| 维度 | Claude in Chrome | 本项目 |
|------|-----------------|--------|
| 通信 | Native Messaging + MCP over stdio | Native Messaging + WS 统一端口 |
| 技术栈 | 闭源（推测 TS + Go Host） | 开源（Go + JS） |
| Profile | ✅ 主 Chrome | ✅ 主 Chrome |
| 后台 tab | ✅ | ✅ |
| Agent 调用 | Claude Code 原生集成 | MCP tools 开放 |
| 平台适配 | 通用 | L3 适配器（小红书/抖音/闲鱼） |

**对标结论**：架构高度相似（都是 L4 Native Messaging），但我们：① 开源可定制；② 深度适配国内三平台；③ 与 user-server 统一端口深度集成。

### 7.3 vs Playwright MCP

| 维度 | Playwright MCP | 本项目 |
|------|---------------|--------|
| 浏览器 | 新开 Chromium/Firefox/WebKit | 复用主 Chrome |
| Profile | ❌ 全新 | ✅ 主 Profile |
| 多引擎 | ✅ 3 引擎 | ❌ 仅 Chrome |
| 网络拦截 | ✅ 一等公民 | ❌ 无（扩展侧没实现） |
| MCP 工具数 | 41+ | 16 |
| 场景 | 测试/验证/CI | 生产运营自动化 |

**对标结论**：Playwright MCP 强在测试能力，弱在 Profile 复用。我们的 Brain + 平台适配器 + 统一端口是差异化优势。

---

## 8. 附录

### 8.1 关键引用

| 引用 | 来源 | 日期 |
|------|------|------|
| Chrome 136+ 远程调试安全变更 | developer.chrome.com/blog/remote-debugging-port | 2025.03 |
| Browser-Use "Closer to the Metal" 放弃 Playwright | browser-use/blog | 2025.08 |
| Stagehand v3 放弃 Playwright 直连 CDP | browserbase/stagehand v3 changelog | 2025.10 |
| Chrome 147+ 默认 profile remote debugging 被静默阻塞 | browser-harness PR #142 | 2026.04 |
| agent-browser 93% context reduction | vercel-labs/agent-browser | 2026 |
| Claude Code 浏览器工具评测 | dev.to/minatoplanb | 2026.03 |
| Five-layer browser automation architecture | cnblogs.com/henu-ws | 2025-2026 |

### 8.2 项目代码引用

| 组件 | 路径 | 行数（约） |
|------|------|-----------|
| BrowserTask Model | `user-server/internal/browser_automation/model/task.go` | 49 |
| HostRegistry | `user-server/internal/browser_automation/service/host_registry.go` | 247 |
| Hand 原语 | `user-server/internal/browser_automation/service/hand.go` | 194 |
| Executor | `user-server/internal/browser_automation/service/executor.go` | 741 |
| BrainService | `user-server/internal/browser_automation/service/brain.go` | 308 |
| Platform Registry | `user-server/internal/browser_automation/platform/platform.go` | 144 |
| Go NM Host | `user-server/cmd/nm-host/main.go` | 211 |
| 路由装配 | `user-server/internal/router/browser_automation_routes.go` | 126 |

### 8.3 修订记录

| 版本 | 日期 | 变更 |
|------|------|------|
| v1.0 | 2026-09-11 | 初稿：技术选型论证 + 决策归档 |
| v2.0（附录） | 2026-09-11 | 附录 A：C1–C8 源码可追溯锚点 + S1–S6 评分落地映射（论证可追溯，不动正文结论） |
| v3.0（附录） | 2026-09-11 | 附录 B：勘误回写（6 条，以代码为准） |
| v4.0（附录） | 2026-09-11 | 附录 C：全链路 9 步外部对标（Browser-Use/Stagehand/chromedp/rod/Temporal/IPI 论文），每步给出采用结论 |

---

## 附录 A. 约束↔源码↔评分可追溯矩阵（v2 深化）

C1–C8 每条约束的源码落点（本轮精读原文）：

| 约束 | 源码锚点 | 落点说明 |
|---|---|---|
| C1 复用主 Profile | 架构事实：无 `--user-data-dir`/`--remote-debugging-port` 启动代码；nm-host 由 Chrome 以 connectNative fork（main.go:1-9 注释） | 寄生用户真实 Chrome，SSO/证书/登录态天然继承 |
| C2 后台 tab 不抢焦点 | `tabs.create({active:false})`（FULL_LINK S5）+ screenshot 先激活是唯一例外（M3 定稿） | AI 与用户共用浏览器不干扰 |
| C3 重启自动连接无弹窗 | nm-host 指数退避 2s→60s（main.go:142-156）+ 扩展重连 2s→30s 上限 5 次（S5）+ 启动 EnsureToken/RestoreAll（routes 122-125） | 掉线自愈，无人工干预 |
| C4 多 Agent 共享会话 | Registry userID→单 conn + 三处外部注入共用 `RunTaskWithRetry`（routes 46-53：MCP tooluse / workflow / Feedback 重试） | 自研 Agent+MCP+workflow 同一入口 |
| C5 无独立浏览器实例 | Hand 约束①不启动子进程（hand.go:9）+ 全链路无 chromedp/rod/playwright 依赖（go.mod 已验证无） | 零服务端浏览器进程，零内存 |
| C6 单统一端口 :8204 | WS 挂 engine 同端口 `/api/browser/host-ws`（routes 118）；默认 `ws://127.0.0.1:8204`（main.go:27） | 无新增端口 |
| C7 Go 主栈 | nm-host 与服务端同 go.mod（gorilla 复用）；扩展仅 JS（Chrome 强制） | 无 Node/Python runtime 依赖 |
| C8 私域单租户 | token 内嵌 userID 归属 + 命令只路由归属 Host（host_token.go:16-17；registry 121-122） | 多租户边界已有，单租户部署是其特例 |

S1–S6 评分落地映射（评分见正文 §4，落地证据见 SOLUTION/FULL_LINK）：

| 方案 | 评分结论 | 落地证据（v1 已验证） |
|---|---|---|
| S1 NM 桥（维持） | 8/8 约束全过 | I1 16/16 对齐零缺口 + I2 token 链完备 + I3 脚本交付；四处 1.2.0 锚点同改 |
| S2 Playwright | 违反 C1/C5/C7 | 仅限 cold-start/E2E 场景（正文口径），本模块不用 |
| S3 chromedp/rod | 违反 C1/C5 | 仅独立 collector tag 可选（正文口径），本模块不用 |
| S4 Selenium | 违反 C1/C5/C7 | 同上，无落地 |
| S5 CDP 直连 | 被 Chrome136/147 封杀（默认 profile 静默拒绝） | trend 表 §2.2；本链 CDP 只用在扩展内 trusted 输入（input.js），不走远程调试端口 |
| S6 Vision | 慢（2-5s/步）贵，L5 | v2 路线仅作截图+LLM 视觉兜底，非主链 |

## 附录 B：勘误回写（以代码为准，与 PROOF §4 互认）
- B1 扩展目录：正文"扩展目录名 bridge"错。`user-web/bridge/`=私信桥；
  自动化扩展=`user-web/browser_automation/` v1.2.0。
- B2 原语口径："16/16"修正为"对外 15（oneof）+ 内部 tab_exists"，dispatch 15+2 终态。
- B3 Chrome147 论据降级为"社区叙事、来源存疑"（官方 changelog 未找到）；Chrome136
  官方 blog 背书成立，结论不受影响。
- B4 定量引用：browser-use 114.1k（非 78k）、Stagehand 24.2k；326KB/11KB、44%、93%
  三数出处未核验，不再作为论据。
- B5 I2 状态：token 轮换标✅（双候选+常量时间比较+reset API 已实现），剩余仅"定时自动轮换"。
- B6 交叉引用：实现层差距分级（G1–G9）与决策（D1–D6）见
  `user-web/docs/platform-base/BROWSER_AUTOMATION_MODULE_TECH_PROOF.md` v1.0；
  本文档只管选型判定，两文冲突处以 PROOF 文件:行号证据为准。

---

## 附录 C. 全链路 9 步外部对标 + 采用结论（v4 深挖）

> 对标时间 2026-09-11；资料来源：browser-use 官方仓库/论文、Stagehand v4 官方文档、
> chromedp/pkg.go.dev（v0.16.0，2,179 引用）、rod 官方对标页、Temporal 官方博客、
> IPI 攻击论文（arXiv 2507.14799）、Cloudflare Browser Run changelog（2026-07）。
> 结论：本链路与两大主流（Browser-Use、Stagehand）同构，"原子原语+智能体+扩展就近
> 执行+a11y 优先"是行业最优解——我方已站在上面，9 步全部"维持现状+吸收增量"。

| # | 链路步骤 | 外部最优方案 | 我方现状 | 采用结论 |
|---|---|---|---|---|
| 1 | 原语层 | Stagehand act/extract/observe＋确定性 page API 混合（v4 口号：Playwright 为测试而生，Stagehand 为 Agent 而生） | Hand 对外 15＋Executor 确定性 loop＋Brain 智能体，同构 | ✅维持；v2 吸收 self-healing（动作失败自动刷新定位） |
| 2 | 元素定位 | a11y tree 是行业标准：Playwright MCP 快照、Cloudflare `/accessibilityTree` 独立端点（2026-07）、Browser-Use DOM+ARIA | @eN refs（SW 内存，MAX400）同构 | ✅维持 a11y-first；v2 加 vision 兜底（canvas/图表类元素） |
| 3 | 可信输入 | CDP trusted input 是唯一绕过合成事件检测路径；Stagehand"runtime lives in the browser"（扩展就近执行，click 21ms、自带 healing） | 扩展内 CDP 直连 input.js（ASCII 码表+CJK insertText+5 步轨迹），不走远程调试端口 | ✅维持；扩展就近执行被 Stagehand 独立验证为最优 |
| 4 | 服务端 CDP 驱动 | rod（新项目推荐，Playwright 级 DX）vs chromedp（零依赖，v0.16.0） | 服务端零浏览器依赖（go.mod 已验证无）——本步不需要 | ✅维持"不用"；v3 独立 collector tag 再选 rod |
| 5 | 任务编排 | Temporal（durable execution 标准）vs 自研 DB 状态机；Temporal 官方承认短工作流可用轻量 task queue | Executor DB 状态机＋五重熔断，符合单体+短任务最优 | ✅维持自研；v3 长任务/跨机再评估 Temporal |
| 6 | LLM 规划闭环 | Browser-Use observe→think→act＋Registry＋SecurityWatchdog；bu-2-0（2026-01）专用决策模型；Cloud 实测 78% | Brain reflect+plan＋JudgeDone＋Hand registry，同构 | ✅维持；v2 借鉴两点：① 小模型蒸馏降成本 ② SecurityWatchdog 式域限制 |
| 7 | 威胁模型 | IPI 论文实证：a11y tree 可被页面 HTML 预埋触发器劫持（GCG 算法，登录凭证外泄/强制点广告） | T1 已登记：brain.go:155 快照无分隔直拼 prompt，防线仅 JudgeDone | ⚠️升级为行业级已确认风险；v2 加固：分隔符包装＋敏感动作二次确认＋allowlist（见 §8 待办） |
| 8 | NM Host 桥 | Chrome 官方原生通道；Go 实现零依赖单二进制 | Go nm-host（211 行，双泵+退避 2s→60s） | ✅维持 Go（C7）；不引入 Python/Node runtime |
| 9 | 回包+鉴权 | req_id over WS = 标准 request-response（等价 JSON-RPC/gRPC stream）；bh_ token＋fail-closed 符合 M2M 最佳实践 | pending chan＋uuid＋四分支 select；双候选常量时间比较 | ✅维持；剩余仅"定时自动轮换"（B5） |

一句话总结：9 步中 8 步"维持现状"（外部最优与我方同构）、1 步（T1 提示注入）
"升级风险+列入 v2 加固"。路线不动（S1 8/8 有效），下一步只做增量吸收。
