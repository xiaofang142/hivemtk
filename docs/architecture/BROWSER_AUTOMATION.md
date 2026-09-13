# 浏览器自动化 — 权威合并文档（Master）

> 版本：v1.2（R24 实施轮状态回写）｜ 2026-09-11 ｜ 本文合并 `BROWSER_AUTOMATION_` 系列全部 9 份文档的有效内容，为该模块**唯一现行入口**；9 份原文已归档至 `docs/architecture/archive/browser-automation/`（各篇处置见附录一）。
> 冲突裁决：本文与任何浏览器自动化文档（含 platform-base 四篇）冲突时，以**本文的文件:行号证据**为准。
> 框架：四问 —— ①我们的要求是什么 ②要解决什么问题 ③同类如何解决（明细）+ 我们的选型论证 ④对照代码缺什么、改什么。
> 纪律：每条结论带出处（文件:行号 或 URL/置信度）；未核验显式标注；定量数字无一手出处者不作论据。

---

## 0. 执行摘要

1. **架构已定且经外部对标复核**：NM Host + WS 单长连接 + MV3 扩展（chrome.debugger）寄生用户主 Chrome。六方案对比 8/8 硬约束唯一通过（§3.1），Chrome 136 官方 blog 实证 CDP 直连路线封杀（§3.2），业界 9 步对标 8 步"维持现状"（§3.5）。选型不再翻案。
2. **实现基线**：Go 模块 ~5,000 行/39 文件五层齐备；对外 15 编排原语 + 内部 tab_exists；双模式执行链（steps 解释 + Brain LLM 循环）共享全部容错/检测/审计设施；三平台适配器注册；v3.37.0–v3.39.0 迁移三件；MCP/workflow/cron/手动四入口。
3. **本轮最大发现（两个 P0）**：G10 铁律 2「trusted 输入」只对 post_comment 成立，普通 click/type 仍走 R17 已证伪的 JS 合成通道；G11 post_comment 可被步级重试二次提交=双发风险，违反 Postiz 写成接口级契约的红线。**二轮最大发现（P1，建议与 P0 同批实施）：G18 并发治理空白——同用户不同任务并发跑时命令在同 Host 交织，平台声明的「写操作串行」风控要求全靠单连接兜底，MaxConcurrentJobs 零消费**。全部差距 G1–G21 与改进 F1–F8/D 系列见 §5。
4. **v1 稳定化三项已闭环**：I1 原语对齐零缺口、I2 token 轮换完备、I3 健康脚本 `scripts/check_browser_host.sh` 交付；I4–I6 挂账（§4.7）。
5. **待拍板两项**：D6（六表迁移独轨 vs 单轨制）、D7（写操作是否开可选人工确认开关——铁律 4 零人工 vs Anthropic 产品级保留发布确认环的张力）。
6. **二轮复审（v1.1）增量**：新增 G17–G21（截图抢焦点违反 C2 例外未声明 / 并发闸缺失 / command_log·llm_plans 无数据保留策略 / cron 无时区固定 / stop 为步边界最终一致）；§3.5 外部对标逐条一手核验——**arXiv IPI 论文与 Cloudflare `/accessibilityTree` 端点升级为确证**，Stagehand「21ms/runtime lives in browser/口号」与 browser-use「bu-2-0/Cloud 78%」四条降级为未核验删除；F5 据代码二验强化为「done 前独立复核」。

---

# 一、我们的要求是什么

## 1.1 产品定义（源自 PRODUCT_DESIGN §1，维持）

**自然语言驱动的 Chrome 操作编排引擎**：用户一句话或结构化工作流描述意图 → LLM 翻译成浏览器原语序列 → MV3 扩展在用户主 Profile 后台 tab 执行。手脑分离三层：Brain（LLM 决策，吃自然语言产结构化计划）/ 计划 schema（纯函数可测）/ Hand（只做原语）。好处：LLM 升级不动执行器，执行器换实现不动上层。

降门槛对照（产品价值本体）：操作已登录站点="一句话直达"（登录态由 P0 前提天然继承，系统不托管密码）；定期抓数="每天 9:00 抓订单导出 CSV"；跨站操作=复用主 Profile 登录态继承。触发三类：循环 / cron / workflow 子节点，另加 MCP 外部 Agent 入口（共四入口，§4.1）。

## 1.2 执行前提 P0（凌驾 C1–C8 的最高约束，2026-09-12 用户定稿）

**整个浏览器自动化建立在使用宿主机 Chrome 默认 Profile 的基础上，目标社交账号（抖音/小红书/闲鱼）由用户日常登录、处于已登录态。** 全链路一切设计与验收以此为地基：

1. **零登录环节**——自动化不携带、不输入、不托管任何账号密码；任务从"操作"直接开始，cookie/SSO/风控指纹全部继承用户人工会话。任何要求新建 profile、独立实例、headless、代登录的方案均判死刑（即 C1/C5 的根因）。
2. **操作的是真人真号**——写入内容直接发布在用户日常使用的账号上，平台风控视图与人工操作同账号同设备同指纹。这是铁律 2（trusted+拟人）、F8 并发闸（同用户写串行）、"不做批量矩阵"红线、以及"同账号禁多端同登"约束（§3.3-Q5 xiaohongshu-mcp 实证）存在的根本原因。
3. **人工与自动化共存于同一浏览器**——用户可能同时在用该 Chrome，故 C2 后台 tab 是硬要求（screenshot 为其唯一声明例外）；自动化不得导航/关闭/干扰用户已开 tab，只操作自己 `tabs.create` 出的 tab。
4. **登录态失效 = 环境故障而非任务失败**——平台未登录时任务应 fail-loudly 归因（DetectBlock/ClassifyError 跳登录页场景），恢复动作是"用户去人工登录"，系统永不代登。

## 1.3 硬约束 C1–C8（一票否决项，受 P0 派生）

| # | 约束 | 违反后果 |
|---|------|---------|
| C1 | 复用用户主 Chrome Profile（SSO/证书/登录态继承） | 每次重登，不可用 |
| C2 | 后台 tab 不抢焦点 | 干扰日常工作 |
| C3 | Chrome 重启自动连接、无弹窗 | 运维爆炸 |
| C4 | 多 Agent 共享同一浏览器会话（自研+MCP+第三方） | 会话互斥 |
| C5 | 不引入独立浏览器实例 | 与 C1 冲突 |
| C6 | 单统一端口 :8204（同一 gin.Engine） | 新增安全面 |
| C7 | Go 后端为主栈（不引 Node/Python runtime） | 技术债 |
| C8 | 私域单租户部署 | 过度设计 |

> C2 存在一个已知例外面：`screenshot` 原语受 captureVisibleTab 平台限制必须激活 tab（→ G17，二验登记）。

## 1.4 产品边界（在范围 ✅ / 出范围 ❌）

✅ LLM 泛操作、主 Profile 后台 tab、定时/循环/工作流触发、MCP 暴露、执行监控+截图+日志。
❌ Playwright 级精细 selector 自动化（脆）、独立浏览器实例、分布式浏览器集群、自建模型推理、**反检测/CAPTCHA 破解（Skyvern 路线）**。
定位红线：个人助理工具——单账号、低频、用户自己的账号，**不做批量矩阵**（合规：各平台 ToS 禁自动化）。

**部署安全口（合并自 LANDING 遗留）**：生产 nginx 必须对 `location /api/browser/` 加 `deny all` 兜底——Host WS 端点虽已有回环 IP+token 双层防护，反向代理层不应放行公网流量。

## 1.5 Charter 四铁律 + 验收 A1–A6（最高裁决，platform-base/PLATFORM_BASE_CHARTER.md）

1. **三平台一视同仁**（抖音/小红书/闲鱼），差异只在 L3 适配器，能力不吹不实现；
2. **UI 自动化、模拟人工**：写操作必须 isTrusted=true（CDP），节奏拟人，检测页面真实状态；
3. **基座=能力供应方，LLM=唯一智能来源**：新场景=新目标、新平台=新适配器、新原语=扩一枚举；
4. **全程全自动** + 失败可归因（ClassifyError）+ 可自愈 + 可审计（command_log）。
验收：A1 三平台读链路 completed / A2 Brain 真机 / A3 拟人生效 / A4 零挂起 / A5 日志可还原 / A6 新平台零基座改动。
⚠️ Charter 现状对照表（09-09 时点）已过时，刷新裁定见 §5.4。

## 1.6 项目级规范（CLAUDE.md）

五层架构 Router→Handler→Service→Repository→Model 零跨层；API 路径 `/api/browser-automation/*`、响应信封 `{code,data,message}`；错误码 0/400/401/403/404/409/500。

---

# 二、我们要解决什么问题（Q1–Q7）

| # | 问题 | 约束下的难点 |
|---|------|------------|
| Q1 | **可信输入**：SPA 框架与平台认可程序化操作 | CDP 直连被禁（C1+C2），唯一合法通道=扩展内 chrome.debugger；dispatchEvent 恒 isTrusted=false 且 React 忽略（R17 实测） |
| Q2 | **行为拟真**：键鼠节奏/轨迹近真人 | 单账号定位下唯一防线即拟真；量化阈值无权威研究（未找到） |
| Q3 | **页面理解**：任意页面喂 LLM，省 token 不失真 | 快照体积、动态 DOM、历史步表示衰减 |
| Q4 | **LLM 决策可靠性**：循环不空转、不自欺、不烧钱 | 幻觉参数、假 done、原地打转、成本失控 |
| Q5 | **写操作成功判定与防重**：平台静默吞 + 超时重试双发 | "点了发布"≠"发布成功"≠"可安全再点"——三件事独立 |
| Q6 | **执行链可靠性**：长连接生命周期、断连归因、失败恢复、审计闭环 | MV3 SW 生命周期、NM 假死、半开 TCP 僵尸连、重启丢定时器 |
| Q7 | **平台异构与知识收口** | 通道三分（DOM/API/未知）；签名协议层与行为层是两条独立线；能力声明诚实性 |

---

# 三、同类如何解决（明细）与我们的选型论证

## 3.1 六方案对比矩阵（结论维持：S1 唯一全过）

| 约束\方案 | S1 NM 桥（当前） | S2 Playwright 新开 | S3 connectOverCDP | S4 Puppeteer | S5 Selenium | S6 Vision |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| C1 主 Profile | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
| C2 后台 tab | ✅ | ✅ | ⚠️ | ⚠️ | ⚠️ | ❌ |
| C3 无弹窗 | ✅ | ✅ | ❌ | ❌ | ❌ | ⚠️ |
| C4 多 Agent | ✅ | ❌ | ⚠️ | ⚠️ | ⚠️ | ❌ |
| C5 不隔离 | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
| C6 统一端口 | ✅ | ❌ | ❌ | ❌ | ❌ | ⚠️ |
| C7 Go 栈 | ✅ | ❌ | ❌ | ❌ | ❌ | ⚠️ |
| **通过** | **8/8** | 3/8 | 1/8 | 1/8 | 1/8 | 3/8 |

S2/S4/S5 同时违反 C1/C5/C7（Node/Python runtime + 新 Profile）；S3 被 Chrome 136+ 封杀（下）；S6 违反 C2（键鼠注入需前台）+慢（2-5s/步）+贵（每步 VLM），**仅作 v2 兜底**（DOM 定位失败时截图+LLM 视觉降级）。

## 3.2 外部一手核验：Chrome 远程调试收紧（维持 NM 的最强论据）

- **Chrome 136（2025-03，官方 blog 一手核验 ✅）**：默认 profile 上 `--remote-debugging-port/pipe` 被静默忽略，必须 `--user-data-dir` 非标准目录 → CDP 直连路线登录态继承不可能。我们不走该端口（go.mod 无 chromedp/rod/playwright，DECISION 附录 A-C5 已验证），**零影响成立**。
- Chrome 147 "硬检查+弹 Allow"：一手 changelog 未找到，**降级为社区叙事不作论据**（勘误 B3）；方向性结论（持续收紧）仍成立。browser-harness 实况=chrome://inspect 复选框一次性开启（非弹窗点击器叙事）。

## 3.3 同类机制源码级明细（按 Q1–Q7）

### Q1 可信输入（同类共识：写操作一律 CDP 级注入，JS 合成仅兜底）
- **Puppeteer（一手源码）**：type 逐字符分流——ASCII 走 `Input.dispatchKeyEvent`（keyDown 带 text/code/windowsVirtualKeyCode + keyUp），**非按键表字符（中文）走 `Input.insertText`**；mouse.click=`mouseMoved→mousePressed{buttons:1,clickCount}→delay→mouseReleased{buttons:0}`。isTrusted=true 的根源=浏览器进程注入。
- **Playwright**：`fill()` 一次写入触发 input；`pressSequentially()` 逐字符完整键盘事件；`insertText()` 只触发 input；`dispatchEvent('click')` 官方定性"simulate by any means possible"=逃生口。
- **browser-use（3752 行 watchdog 一手源码）**：点击=CDP 三步 + `_check_element_occlusion`（DOM.resolveNode+Runtime.callFunctionOn hit-test）+ scrollIntoViewIfNeeded；输入=逐字符 keyDown→sleep 5ms→char（注释"crucial for text input"）→keyUp；输入后 Runtime.callFunctionOn **补发** input/change/blur + React fiber/Vue 定向触发（合成=兜底，主通道仍 CDP）。⚠️ 源码 `defineProperty(ev,'isTrusted',{value:true})` 一行无效——反证 JS 伪造 isTrusted 走不通（MDN 同）。
- **Midscene bridge 模式（一手）**：`chrome.debugger.attach(tabId,'1.3')`，cdpInput.ts 文件头自注 **"From puppeteer cdp/Input.ts with modifications"**——与我们扩展同型；输入三档 `legacy|sequential|bulk`（bulk=insertText，与 delay 互斥）。
- **xiaohongshu-mcp（同场景最强参照，一手源码）**：输入=Focus→WaitEnabled→WaitWritable→逐码点 `page.InsertText`（CDP）+每字符 LogNormal(30–400ms)；小红书评论框只认 input 事件故不产生 keydown/keyup。
- **Anthropic browser use tool**：模型决策+本地驱动执行（"nothing runs on Anthropic's side"）；a11y-ref 与视觉坐标分两个工具集；**产品级对发布/购买类动作保留用户确认环**（→ D7 张力）。实际输入通道未公开（推断 chrome.debugger）。

### Q2 行为拟真（同场景标杆参数表）
- **xiaohongshu-mcp humanize 包**：LogNormal 11 相位（Keystroke 30–400ms / ClickHold 45–250ms / BeforeClick 80ms–1s / PointerSettle 200–1200ms / AfterNavigate 600ms–6s / AfterInteract 2–3s…）；**三次贝塞尔轨迹**（步数=距离/10 夹 10–40、每步 5–9ms、控制点垂直偏移 ±5–15%、easeInOut）+ 落点抖动（元素 15% 与 8px 取小均匀偏移）+ 点击前 hit-test。
- Puppeteer/Playwright 提供 delay/steps 机械参数但默认 0=不拟真（**测试框架不做拟真，生产才做**——分野本身是论据）；browser-use 仅固定微延时无轨迹分布；拟真阈值无权威研究→参数只能抄同场景实测值。

### Q3 页面理解
- **browser-use**：`[index]<tag/>` 交互树 + `*[index]` 上一步后新元素标记 + `|SCROLL|`/`|SHADOW|` 前缀 + 视口内裁剪 + 40k 字符截断；截图=GROUND TRUTH，**历史步截图降为 4px 占位图**；system/state `cache=True` + step_info 置尾保 prompt cache。
- **playwright-mcp（一手 README+源码）**：a11y YAML 快照 + `e<n>` ref（`aria-ref` 解析回元素，iframe 前缀 f1e3）；**每动作响应自动附带新快照**（500ms settle）；ref 失效=硬错误强制重拍；视觉坐标仅 `--caps=vision` opt-in；官方立场：MCP 留给"持久状态+富内省+迭代推理的专门 agent 循环"，编码类 agent 推荐 CLI+SKILLs——与我们双模式并存互证。
- 我们 @e{N}+MAX_NODES=400+隐藏过滤：形态同构，缺三项低成本增强（新元素标记/历史降级/settle 约定，→F6）。

### Q4 LLM 决策可靠性
- **browser-use（一手协议全貌）**：输出 required=[evaluation_previous_goal, memory, next_goal, action]、action 永不空；**历史压缩**：每 25 步且全文超 40k 字符→小模型总结 ≤6k `<compacted_memory>`（"未见显式成功确认不得标 completed"+注入附 unverified 警告）；max_actions_per_step=5 **页面变化即截断剩余动作**（page-changing 置尾）；max_failures=5 + `final_response_after_failure`（超限强制收尾调用产部分结果）+ fallback_llm 整局切换；**循环检测三档**：动作归一化哈希（search 按 token 排序/click 按文本）20 窗重复 5/8/12 三档 nudge + 页面指纹（url+元素数+DOM SHA）连 5 步不变=停滞 + replan nudge@3；**pre_done_verification 六条**：重读需求逐条对照、**每个输出值必须原样出现在工具输出中（禁先验知识补洞）**、阻塞即 success=false、"部分结果+false 胜过谎报成功"；is_successful 只是自评，"重要外部动作须对目标系统独立核验"。
- **Stagehand**：act/observe/extract+agent 四原语；**selfHeal=true 默认**（缓存 selector 失效→回炉 LLM 重定位，非通用重试）；恢复四级=observe 重验→退避→agent fallback 补多步→清缓存重探索；服务端 cache key=instruction+a11y 树+options+URL（variables `%name%` 占位，key 不含值→一次 priming 永久命中）。
- **Midscene**：replanningCycleLimit=20（UI-TARS 40）；plan 失败→当次回退 AI 规划 + **清 stale flow 不回写脏计划**；查询结果永不缓存；每 run 自包含 HTML report（可 split JSON）。

### Q5 写操作成功判定与防重（标杆=Postiz，一手源码）
- **三段式契约**：`post()→{pending, pendingData}`；`checkPostStatus()→pending|ready|completed`；finalize 执行剩余变更；pendingData=provider 不透明私有状态，泛型代码绝不解读。
- **防双发接口级红线**：finalize 真正落地后 check 必须 completed、**永不再回 ready**（否则"结果未知"重试=重复发帖）；唯一豁免=幂等变更。
- **读写分治重试**：check 只读→自动重试 3 次/10s；postPending/finalize 不可逆→**maximumAttempts:1 + 心跳判因矩阵**（无心跳=从未启动可安全重试；有心跳后超时=结果未知→markUnconfirmed 通知用户"可能已发布，先查账号再重发"）；finalize 前先持久化 pendingData。
- **xiaohongshu-mcp**：`waitCommentRendered(content, 4s)` 每 300ms 轮询 `.comments-container` innerText 含目标文本，未渲染判失败且文案明写"可能账号被限制"；运营约束实测（单账号禁多端同登、日发帖 ~50 上限、先查违禁词）。
- **DBOS**：step 返回值 checkpoint 进 Postgres，崩溃后从最后已完成 step 重放、已完成不重执行——"推理记忆留 LLM 层、外部事实判定交 durable 层"。
- **MediaCrawler/xhshow（协议层）**：签名输入=a1 cookie+uri+参数；会话状态=固定页面加载时间戳+单调计数器（SessionManager 自述效果待验证）；小红书已从"浏览器上下文取签名"迁 xhshow 纯算、抖音 a_bogus 仍绑真实页面上下文——**平台策略分裂无统一方案**；461/471→读 Verifytype 头抛验证码不绕过、300012=IP 封换代理、300011=账号限制不重试。

### Q6 执行链
browser-use 1/2/4s 重连 watchdog；Midscene 扩展 already-attached 容忍+detach 重 attach 重试 2 次；**Playwright actionability 五项**（visible/stable/enabled/editable/receives-events，按动作分配检查表）被 xiaohongshu-mcp 与 browser-use 各自重新实现——裸 CDP 缺这层是业界公认刚需。

### Q7 平台异构
分层同构先例：Postiz 30+ provider 注册表 / Mixpost SocialProviderManager（新平台=一个目录）；Postiz optional 能力语义；契约默认失败。写链路通道实测：小红书 DOM 成熟（15.7k★ 一年+）、抖音公开签名 API（a_bogus 2024-09 大版本迭代先例）、闲鱼未公开（真机抓包前置）。

## 3.4 我们的技术栈论证（一行一结论，源自 FULL_LINK S0–S8）

| 技术栈 | 结论 | 一句话论证 |
|---|---|---|
| Vue3+Vite+Pinia+ElementPlus | 维持 | 与 manage 同栈、控制台无重交互、换栈零收益 |
| Gin | 维持 | 全仓统一；Router 零逻辑合规五层铁律 |
| GORM 6 repository | 维持 | 六表够用（schema §4.4） |
| Gorilla WebSocket + uuid req_id | 维持 | 事实标准；pending chan 惯用法；**缺心跳是 gap 不是库的锅（G4）** |
| Go NM Host | 维持 | 与服务端同 go.mod、单二进制分发、stdlib 直持帧操作（Python/Node 被否） |
| MV3 SW + connectNative | 维持 | MV2 已淘汰无备选；port 保活+重连+冷启动锚点对冲 SW 短命；SW 内存 refs 导航即失效=已知代价 |
| chrome.scripting.executeScript | 维持 | 官方唯一注入通道；序列化闭包铁律代码内明示 |
| chrome.debugger CDP | 维持 | trusted 唯一解；横幅/infobar 56px 已对冲；**覆盖范围不足（G10）** |
| @eN a11y snapshot | 维持 | 对标 browser-use/PMCP；refs 重排用 click_near 锚文本对冲 |
| LLM reflect+plan+judge | 维持 | 四件套对标齐、五重熔断齐；细节差 G14/G15 |
| L3 init 自注册表 | 维持 | database/sql 同构 Go 惯用法；重复注册 panic 防呆；Get 返 error 防空 |

被否备选：Playwright/chromedp/rod（C1/C5/C7）、React 重写、Python/Node Host、Vision 主链。

## 3.5 全链路 9 步外部对标（DECISION v4.0 附录 C，本轮二验修正版）

> 本轮对附录 C 引用逐一外查：**2 条升级为已核验一手、3 条定量/口号降级为未核验**（不删定性结论，剥离伪论据）。

| 步骤 | 修正后表述 | 核验状态 |
|---|---|---|
| 1 原语层 | Stagehand act/extract/observe+确定性混合，与我方 Hand+双模式同构 | ✅ 定性维持；「Playwright 为测试而生」口号未见于官方文档，删除 |
| 2 元素定位 | a11y-first 行业标准：**Cloudflare Browser Run `/accessibilityTree` 独立端点 2026-07-07 官方 changelog 实证**（interestingOnly/root 参数，面向 agent 免解析 HTML/截图） | ✅ **一手确认（本轮）** |
| 3 可信输入 | CDP 唯一解维持；Stagehand 确有扩展 service worker 就近执行（官方原文 "execute in the Stagehand service worker, not the webpage"，experimentalBatch）；**"click 21ms" 与 "runtime lives in the browser" 未见于官方文档，删除** | ✅ 机制一手 / ❌ 数字降级 |
| 4-6/8-9 | 服务端零依赖 ✅、自研 DB 状态机 ✅、NM 桥 ✅、req_id+bh_ 鉴权 ✅ | 维持（此前已核） |
| 6 LLM 闭环 | browser-use observe→think→act+SecurityWatchdog 借鉴两点维持；**「bu-2-0 专用决策模型」「Cloud 实测 78%」官方文档索引均无此二者（模型页现仅 GPT-6 Astra），叙事来源不明，删除**；**四参数默认值本轮经官方参数页一手核验：max_actions_per_step=5、max_failures=5、step_timeout=180、final_response_after_failure=True——与本文 §4.6 对标值一致** | ⚠️ 数字删除 / ✅ 参数升级一手 |
| 7 威胁模型 | IPI 论文 **arXiv 2507.14799 本轮一手确认**：《Manipulating LLM Web Agents with Indirect Prompt Injection Attack via HTML Accessibility Tree》——GCG 优化对抗触发器嵌入 HTML，agent 经 a11y tree 解析即被劫持（窃取登录凭证/强制点广告，基座 Llama-3.1+Browser Gym）；我方 T1（brain.go:155 快照无分隔直拼）**由"社区风险"升格为"论文一手实证的攻击面"** | ✅ **一手确认（本轮）** |

一句话总结维持：9 步中 8 步"维持现状"、1 步（T1）"升级风险列入 v2 加固"——且 T1 的升级现在有一手论文背书。

---

# 四、实现技术明细（定稿事实，源自 FULL_LINK + SOLUTION v2）

## 4.1 全链路九步与入口

```
S0 前端控制台(7 路由/27 api) → S1 Gin 路由(JWT 25 端点 + WS 独立 token+回环)
→ S2 Executor(steps 解释 / Brain 循环) → S3 Hand(15+1)→HostRegistry(req_id)
→ S4 WS /api/browser/host-ws → nm-host(Go 子进程,双泵,退避 2s→60s)
→ S5 NM 4 字节 native-order 帧(≤1MiB/4GB) → 扩展 SW connectNative
→ S6 primitives.js dispatch → executeScript 注入 / CDP trusted / captureVisibleTab
→ S7 L3 平台适配(Locators/DetectBlock/ClassifyError/CommentLocators)
→ S8 回包原路 → step.result/session 状态机/command_log 审计 → 前端 Monitor/Detail
```
触发四入口：手动 RunTask / cron（RestoreAll 启动恢复）/ MCP `browser_*`（tooluse）/ workflow `browser_task` 节点；失败自动重试 FeedbackService→RunTaskWithRetry。
**bridge/ 目录 41 文件=私信桥（DM 摄取引擎，无 nativeMessaging/debugger），与本链无关，勿混淆**（勘误 B1 定稿）。

## 4.2 命令往返时序（一次 click 的帧旅程）

前端 POST run → Executor → `Hand.click(@e7)` → Registry.Request（uuid req_id、writeMu 串行、写 deadline 10s）→ WS 帧 → nm-host writeNativeFrame（4B native-order 头+JSON）→ NM → 扩展 dispatch（@e7→CSS 解析→执行）→ 回帧原路 → readLoop 按 req_id 投 pending chan → recordResult 落库 + command_log 两帧（direction=command/event，seq session 局部单调）。
超时/中断四分支 select：ctx 取消 / 连接关闭（ErrHostOffline→409 引导装扩展）/ 超时（`Host 命令超时`）/ 回包（!ok→error）。超时后 late 回包丢弃无泄漏。
Hand 三约束（hand.go:9-11）：不启动子进程 / 单 Host 连接内串行（对端约束非简化）/ 每命令超时（默认 30s；markdown 60s、post_comment 45s、wait 类按参数放宽）。

## 4.3 鉴权与并发正确性

- Token：`bh_<userID>_<32hex>`（16B crypto/rand），userID 内嵌使握手即绑定归属（C8 多租户边界）；KV 键 `browser_host_token`/`_prev`；Rotate 老值滚入 _prev 宽限；Validate 双候选+`subtle.ConstantTimeCompare`，**KV 空 fail-closed**；EnsureExists 防首部署卡死；WS 双防护=token+回环 IP。
- Registry：同用户旧连接顶掉（go old.close()+指针比对防误删）=最后上线者生效，旧端 running session 置 failed 非静默接管（符合"不静默"）；断连钩子 10s ctx FailRunningByUser。
- 单 Host 串行：同用户命令天然串行无写竞争；跨用户 conn 隔离。**缺口=无应用层心跳（G4）**：register 后 readDeadline 清零，半开 TCP 僵尸连挂到命令超时且清理钩子不触发。

## 4.4 六表 schema（migration v3.37/38/39，**版本化独轨**，不在 AutoMigrate 全局列表——D6 待拍板）

| 表 | 关键列 |
|---|---|
| browser_tasks | task_type(one_shot/loop/cron/workflow)/status(draft→ready→running→paused→done/failed/archived)/steps jsonb/brain_mode+brain_goal/loop_count=1/delay_ms=1000/timeout_sec=120/depends_on(mode all_done/any_success)/retry(默认关/delay 300s/max 3)/platform(默认 xiaohongshu)/user_id+account_id |
| browser_sessions | chrome_tab_id/status(created→active→completed/failed/stopped)/snapshot/total+success+failed_steps/hand_latency_ms(恒 0,G9)/console_errors(不采集,G9)/extracted_data/final_screenshot_url/llm_summary(completed 与 failed 都写) |
| browser_steps | step_index/action/target(@eN 或 selector)/params jsonb/status(pending→running→success/failed/skipped)/result jsonb/duration_ms/error_msg（注释列 11 action 滞后于 15，行为以 dispatchStep 为准=已知文档债） |
| browser_command_log | session/task/step_id/seq(session 单调)/direction(command/event/judge)/action/payload 全文/duration_ms/ok；**无 Update/Delete=append-only**；查询端点缺失=G1 |
| browser_llm_plans | task_id/goal/snapshot/steps/reasoning(脱敏截断)/model/token_in/token_out（**无 session_id 列=G3**） |
| browser_cron_triggers | task_id(uniq)/cron_expr(六段)/enabled/next_run_at/last_run_at |

## 4.5 原语口径（定稿，全仓统一）

**对外编排=15**（dto oneof：open_tab/click/type/click_near/post_comment/snapshot/markdown/screenshot/wait/wait_for_selector/scroll/extract/assert/query/close_tab）；dispatchStep=15+2 终态；扩展 primitives.js=16（+内部 tab_exists 不对外编排）；Hand=17 方法（+ensureReady 辅助）。旧文档"13/11/16/16"全部按此读（勘误 B2）。

## 4.6 执行引擎常量速查（排障用）

熔断：maxBrainIterations=40 / plan 连败 3（空 plan 计入）/ 动作连败 5 / token 预算 200k（BRAIN_TOKEN_BUDGET）/ wall-clock=TimeoutSec+30s（R22：ctx 链在 LLM/DB 栈曾不生效，session132 实测 11min+ → 双看门狗）。循环指纹连续 3 轮同序列注 nudge / judge fail-open 连续 2 次不放行 / retry≤3、backoff 1s–10s 钳位 / LLM MaxTokens plan 4096·judge 512·轻量 256 / history≤24 滑窗 / 步间 humanizedDelay=base±30% 均匀 / 单步指数退避 backoff·2^(attempt-1) 默认 1000ms。
越界钳位（扩展）：wait_for_selector timeout 1–60s / scroll 0–20000 / extract 单 key ≤100 节点 / markdown ≤64KiB / wait ≤60s。
错误码要点：ErrHostOffline→409；`chrome_write` 错误帧=NM 通道坏快速失败；平台未注册/能力缺实现=error 直返（fails-loudly）；disconnect→终止+人工介入；401/403 LLM 快败不烧预算；429/5xx 退避；未知保守不重试。
版本锚点：**三处 1.4.2 必须同改**（扩展 src manifest + dist manifest + nm-host hostVersion；R24 升 1.3.0=F1/F3/F4，R25 升 1.4.0=F2② 三段式子命令+F6 新元素标记+G9 captureVisibleTab 修正，R26 升 1.4.1=注入竞速 deadline，R27 升 1.4.2=nm-host shutdown 控制帧消费端）；SW ScriptCache 陷阱下 host/status 版本号=新代码生效判据；check_browser_host.sh 从源码动态提取版本、无硬编码。协议动作口径：编排/LLM 可见 15 步动作不变（post_comment 仍是唯一对外写入口），扩展协议面=15+3 内部子命令（comment_prep/send/verify）+tab_exists 内部+__host_shutdown__ 控制帧（nm-host 拦截不转发扩展），一站式 post_comment 协议 case 已删（单一路径防分叉）。

## 4.7 v1 稳定化收口状态与挂账

- **I1 ✅**：扩展 dispatch 对外 15 全对齐零缺口（含 CDP trusted 于 post_comment），bridge 无关论证实毕。
- **I2 ✅**：token 生成/轮换双候选/常量时间/fail-closed/reset API 全链完备（勘误 B5）；剩"定时自动轮换"（现手动）。
- **I3 ✅**：`scripts/check_browser_host.sh`（171 行，5 级检查：扩展双 manifest 版本→hostVersion 锚点→Host 二进制+conf→NM 注册表→host/status 在线；--offline 模式）。
- **I4 重连续跑 P2**（挂账：Host 重连后从最后成功 step 续跑，需 Executor checkpoint 位点，依赖 F2 的 finalize 与 M4 事件日志重放）。
- **I5 审计完善 P2**：G1 查询端点（D1 ✅R24）+ **导出端点 ✅R27**（GET /sessions/:id/export 单请求归并会话+步流水+命令流+LLM 成本账，前端 Monitor「导出审计包」落盘 JSON；真机验证 session195 steps5/logs10 + session188 plans18·snapshot 不带出）+ 截图已在导出包（final_screenshot_url 字段）；console 采集维持**不做**（G9 决策=该列恒空已从模型删除，注入页 console 读取属注入劫持面扩张）。
- **I6 多 Host P3**（Register 改 userID→[]conn+负载选择，预留）。
- T1 **提示注入面（行业级已确认风险）**：brain.go:155 快照与 goal 仅换行分隔、无不可信数据标注；现有防线=动作白名单（LLM 只能下发 15 原语，blast radius 有界）+JudgeDone；v2 加固=分隔符包装+敏感动作复核（→ 与 F5 同批）。

---

# 五、对照代码：缺哪些、改哪些

## 5.1 已实现清单（15 项，逐项亲手复验，证据链保留）

双模式执行链共享容错（executor.go:149/237/436/520）｜15 原语+双端分发｜步级 retry/退避/continue-on-error｜DetectBlock 步后+轮后接线+disconnect 终止（executor.go:202,419,490）｜步间 ±30% + CDP 对数正态/轨迹/hold（cdp/input.js:35-53,136-162）｜post_comment 三段式 fails-loudly（primitives.js:429-452）｜ClassifyError 四分类（platform.go:28-36+三平台）｜command_log append-only 三类帧｜reflect 四字段跨轮回喂+循环指纹+JudgeDone+连续 fail-open 拦截（brain.go/executor.go:296-338,423）｜双看门狗｜Brain 可靠性 P0 全项（rune 预算/错误分类/clamp/去竞争，单测 5 组）｜token 预算熔断｜WS per-user 路由+双层鉴权+断连清理｜NM 帧协议+退避重连｜依赖检环/URL 白名单/幂等/cron 六段/MCP+workflow 双入口。
测试现状：Go 3 文件全在 service（ValidateURL/cron/ParseSteps/预算/token）；扩展 vitest 4 文件 23 it；**未覆盖**=Executor 主循环/dispatchStep/HostRegistry/repository/三平台适配器/cdp/input.js（G8）。

## 5.2 差距总表（G1–G21，全量）

| # | 级别 | 差距 | 证据 | 改进 |
|---|------|------|------|------|
| **G10** | **P0** | **铁律 2 只对 1/4 写原语成立：click/type/click_near 仍 JS 合成（el.click+dispatchEvent，isTrusted=false、React 忽略），R17 修复只落 post_comment 未回收** | primitives.js:9-101 通读；同题同类全走 CDP（§3.3-Q1） | **F1 ✅R24**（扩展 v1.3.0：probe→CDP 主通道，DOM 兜底显式标 channel） |
| **G11** | **P0** | **post_comment 被步级重试二次提交=双发：verify 超时态（R17/R19-6 实测形态）=结果未知，重试违反 Postiz maximumAttempts:1 红线** | executor.go:453+（retry 对所有 action，LLM 可下发≤3） | **F2① ✅R24**（isWriteAction 强制 retries=0）；**F2② ✅R25**（三段式拆回 Go：扩展 comment_prep/send/verify 子命令、一站式协议删除；Go prep→send→finalize 只读轮询+证据落库；真机 session195 实证 send 超时结果未知态 finalize 正确归因成功） |
| G1 | P0 | command_log 有写无读：ListBySessionID 零调用、无 GET 路由——铁律 4 审计链半截 | session controller 仅 5 端点 | **D1 ✅R24** |
| G2 | P0 | query attr 三层断链：oneof 声明/扩展支持/Go StepParams+hand.query 不透传 | dto:21 vs primitives.js:343-349,461 vs hand.go:131 | **D2 ✅R24** |
| G3 | P1 | v3.40 迁移不存在、llm_plans 无 session_id、summary token 无计量=成本账半截 | migrations 目录最大 v3.39 | **D3 ✅R24**（v3.40.0 迁移已建，llm_plans session_id/kind+judge/summary token 落库） |
| G4 | P1 | WS 零应用层心跳，僵尸连挂满命令超时且清理不触发 | controller/host.go:110 | **D4a ✅R24**（pingLoop 30s+readDeadline 90s） |
| G5 | P1 | 重试定时器内存态，进程重启丢 | feedback.go:126 | **D4b ✅R24**（next_retry_at 列+扫描认领） |
| G12 | P1 | 鼠标轨迹=固定起点 (x-20,y-45) 5 步线性 8ms——低于标杆一档（贝塞尔 10–40 步+落点抖动） | cdp/input.js:140-148 | **F3 ✅R24**（贝塞尔+随机起点+落点抖动+起点记忆，单测断言轨迹不恒定） |
| G13 | P1 | 写操作前无 actionability 检查（五项全缺，坐标赌点） | injClick 无 visible/stable/hit-test | **F4 ✅R24**（actionabilityCheck：visible/box/disabled/遮挡；stable 单帧限制如实注明） |
| G14 | P1 | judge 的证据是**自报摘要**：finalState=ExtractedData 截 2048 字符+history 末 6 句（executor.go:316-317），judge 全程不接触页面——可被自述骗过，比"缺 grounding"更严重（本轮二验升级） | brain_prompts.go buildJudgePrompt + executor.go:316 | **F5+T1 ✅R24**（done 前重拍快照作证据+`<page_snapshot>` 分隔护栏） |
| G15 | P2 | 无历史压缩/无新元素标记/一步多动作无页面变化截断 | executor history 平铺 | **F6a ✅R24**（open_tab/click-navigated 即截断本轮剩余）；**F6 余项 ✅R25**（历史压缩=滑窗 24+溢出折叠台账 `[已折叠 N 步: click×21…]` 首行回喂，零 LLM 成本；新元素标记=SW 按 tab 基线 diff、新行 `*` 前缀，导航/开页清基线） |
| G16 | P2 | 重试未按错误类型分线（bad_body 0 试/refresh 终止/retry 按参数/disconnect 终止） | dispatchStep 失败路径不看 ClassifyError | **F7 ✅R24**（stepErrRetryable 三线分治） |
| **G17** | P1 | **编排 `screenshot` 原语硬编码抢焦点**：`hand.screenshot(..., true)` activateFirst=true（captureVisibleTab 只能截激活 tab 的平台限制），但意味着任何含 screenshot 步骤/轮次的任务会**周期性切到用户前台**——C2 的例外面从未向用户显式声明（executor.go:586→primitives.js:469） | C2 vs 机制必然 | ✅R24 定稿修正：不激活会静默截错页（假内容），故激活保留为正确性必需；收口=Brain 轮内服务端硬拒 screenshot（G17→prompt 护栏+executor 闸）
| **G18** | P1 | **并发治理空白：平台级串行只是"单连接交错恰好兜住"**：MaxConcurrentJobs 声明（platform.go:44"写操作串行是风控要求"）全仓仅 controller 展示（platform.go:22,37），执行链零消费；幂等只防同任务重跑（task.go:289 CountRunningByTask），**不同任务/同任务手动+cron 并发跑无 per-user 闸**（CountRunningByUser 死代码），两 session 命令在同 Host 交织——帧级串行、逻辑级并发，风控节奏被打乱且写互踩 | 二验新增 | F8 ✅R24：ErrUserBusy+CountRunningByUser 闸（cron 命中=本轮跳过）|
| **G19** | P2 | **数据保留治理空白**：command_log payload 全文+llm_plans 每 Brain 轮一行（快照/摘要大字段）无界增长，全模块 grep 无 retention/purge/清理任务（二验）——cron 常态运行下表体积不可控 | 二验新增 | ✅R24 retention.go：BROWSER_AUDIT_RETENTION_DAYS 默认 90，command_log 分批裁剪+plans 快照置空 |
| **G20** | P2 | **cron 时区=服务器进程 Local**：六段表达式注册无 WithLocation（cron.go 全文无 LoadLocation 调用，二验），容器/部署机 TZ≠用户期望时"每天 9:00"静默错点，任务表无时区列 | 二验新增 | ✅R24：triggers.time_zone 列+CRON_TZ= 前缀+UI 选择器 |
| **G21** | P2 | **stop 为"步边界生效"**：stopCh 机制完整（sleepInterruptible/stopFired，二验通过——非缺陷），但在途命令帧不取消（≤45s 后生效）；UI 需按"最终一致停止"表述 | 二验新增 | ✅R24 文案：停止提示"步边界生效" |
| G6 | P2 | 文档"四件套 Locators/Recipe/Verifier/Signature"无 Recipe/Signature 接口方法；三适配器纯静态 | platform.go:39-60 | **改文档不改代码**（不为 M3 预建空接口） |
| G7 | P2 | 三平台写链路如实：小红书技术全通但平台静默吞（基座侧无优化项）；抖音 M3（a_bogus 零代码）；闲鱼接口未找到 | §3.3-Q7 | 维持"不吹不实现" |
| G8 | P2 | 测试盲区 | §5.1 | D5：**input.js 事件序列单测提为 F1/F3 前置** |
| G9 | P2 | 死代码/恒 0 字段清单（tabExists 无调用/LlmPlanID/Title/LatencyMs/ConsoleErrors/BuildLoopNudge/旧 GeneratePlan 转发壳/captureVisibleTab 参数笔误；**CountRunningByUser 例外——非删除，F8 并发闸即其消费方**） | PROOF §4.3 G9 明细 | **✅R25 全收口**：死壳三件删、Hand.tabExists 删、BrowserSession.Title/LlmPlan/HandLatencyMs/ConsoleErrors+BrowserTask.LlmPlanID 字段删（DB 列按 D6 保留）、UpdateTitleAndSnapshot→UpdateSnapshot/UpdateMetrics 去恒 0 参/UpdateArtifacts 去 consoleErrors、BuildLoopNudge 接线 nudge、captureVisibleTab 参数笔误修、UI 去 Hand 延迟展示项 |

## 5.3 改进方案（F1–F7 明细）

- **F1（P0）**：click 加 trusted 主通道——injClick 定位拿坐标→cdpInput.clickAt（Midscene 同型背书）；type 的 contenteditable/input 分支改 cdpInput.typeText 逐码点 insertText；执行器对 click/type 默认 trusted，DOM 合成降级兜底；顺带 F4 的 ensureInteractable 前置（trusted 需要坐标，坐标先证明可点）。
- **F2（P0①→P1②）**：① 即刻：Go 对 post_comment 及未来写原语服务端强制 retries=0（~3 行）；② 正确版：三段式拆回 Go（verify 只读轮询可重试、提交不可逆禁重试）+ finalize 落 commentId/时间戳（闭环 DESIGN:81-84 欠账与 I4 的续跑位点）。
- **F3**：贝塞尔轨迹+随机起点（自上一 mousemove 位置）+落点抖动，参数照抄 xiaohongshu-mcp；扩展侧纯改动，单测断言两次调用轨迹不同。
- **F4**：ensureInteractable 统一前置：可见→两帧 stable→enabled/editable→elementFromPoint 命中自身；不满足滚入重试→超时抛 not_interactable（ClassifyError=retry 类）。
- **F5（二验修正版）**：judge 现喂自报摘要（executor.go:316，见 G14）——改为 **done 前独立复核**：执行器重新 snapshot/query 一次，judge 拿"真实页面文本+声称值"逐值比对（对标 browser-use judge 用轨迹+截图的语义）；叠加程序化包含校验（done 的 extract 值必须出现在落库 result/新快照中，零 LLM 成本先拦明显幻觉）；与 T1 分隔符包装合并为"prompt 卫生"一批。
- **F6**：动作后页面指纹变化→截断本轮剩余 steps（executor 一行级，防错位执行）→ 快照 diff 打新元素标记 → 历史压缩（阈值照抄 25 步/40k→6k）。
- **F7**：dispatchStep 失败路径接 ClassifyError 四线分治（与 F2 同文件同批）。
- **F8（二轮新增）**：并发闸——RunTask 入口直接接上已存在却零调用的 `sessionRepo.CountRunningByUser`（repository/session.go:213，G9 死代码清单同址——**现成轮子，接上即用**），闸值消费 MaxConcurrentJobs（默认 1；跨平台任务可按 platform 维度放宽），超限返回 409 并提示等待/停止既有 session；cron 触发命中同样走此闸（跳过本轮+日志，不排队堆积）。是 G18 的唯一根治，I6 多 Host（一 user 多 conn）前必须先落地。
- D 系列（维持 PROOF v1 方案不变）：D1 日志 GET+命令流面板 / D2 attr 贯通 / D3 v3.40 成本账 / D4 心跳+重试持久化 / D5 补测顺序。

## 5.4 勘误定稿表（原 9 份全部散点合并，一次生效）

| # | 勘误 | 处置 |
|---|------|------|
| B1 | "扩展目录名 bridge" | 错。bridge=私信桥（无 debugger/nativeMessaging）；自动化扩展=`user-web/browser_automation/` v1.2.0 |
| B2 | 原语数 13/11/16/16 各说各话 | 统一：对外 15+内部 1（§4.5） |
| B3 | Chrome 147 论据 | 一手未找到，降级社区叙事不作论据；136 官方 blog 实证维持 |
| B4 | 定量引用 | browser-use 114.1k / Stagehand 24.2k / xiaohongshu-mcp 15.7k（当日核）；326KB·44%·93% 三数无一手出处→删除不引 |
| B5 | I2 标未完成 | 实已完成（双候选+常量时间+reset API），剩定时自动轮换 |
| B6 | Charter 现状表（09-09） | 步间抖动/知识注入/DetectBlock 已完成刷新；**但铁律 2 应记 ⚠️ 部分达成**（G10：trusted 覆盖不足，v1.0 PROOF 表述"已有 CDP trusted"过宽一并修正） |
| B7 | DEEP_RESEARCH 帧上限 64MiB/小端 | LANDING 正确（4GB/native order）与代码一致；原文不回改，以本文 §4.6 为准 |
| B8 | BRAIN_SPEC P1-1 视为完成 | 半截（G3） |
| B9 | DESIGN.md 状态行"待确认实施" | Charter 已拍板+M1/M2 已实施→过时，已随本文归档说明合并 |
| B10 | Host 启动方向（v1 文档"Go 启动子进程"） | Chrome fork Host 子进程、server 绝不启动（LANDING A5 定稿，代码一致） |

## 5.5 决策与不做清单

**D 系列实施状态（R24，2026-09-11）**：D1/D2/D3/D4a/D4b/F1/F2①/F3/F4/F5+T1/F6a/F7/F8/G17/G19/G20/G21/D5（Go 三套+扩展 input.js/primitives 专项，vitest 30 过）**全部代码落地**；单测/编译/迁移测试全绿。**R25 挂账四项全闭（2026-09-13）：F2②/F6 余项/A1–A6 真机回归/G9 死代码清扫**，详见 §5.6。**待拍板**：D6 六表"版本化独轨 vs AutoMigrate 单轨制"（倾向：存量表续版本化、新表单轨，不动线上 DDL）；**D7 写操作可选人工确认开关**（默认关）——铁律 4 零人工 vs Anthropic 产品级保留发布确认环 + T1 注入劫持发布面的现实威胁，二者张力需定夺。
**明确不做（引用同类证据）**：JS 伪造 isTrusted（browser-use 源码反证+MDN）；合成事件作主通道（全部同类）；批量矩阵/多账号池（产品定位+xiaohongshu-mcp 同账号禁多端实证）；预建 Recipe/Signature 空接口（G6）；tiktoken（SPEC 已否决）；视觉通道（等 DOM 失败率数据，v2 兜底位已留）；断点续跑（M4 挂账，command_log 已是重放源）；Temporal 引入（单体短任务自研 DB 状态机达标，v3 跨机再评估）。

## 5.6 R25 实施轮（2026-09-13，扩展 v1.4.0，全链路功能逐项真机回归）

R24 挂账四项全部闭合并经真机全链路验收（详细链路与新缺陷论证见 `BROWSER_AUTOMATION_R25_CHAIN_AUDIT.md` §4）：

- **F2②（G11 正确版）**：三段式拆回 Go。扩展协议新增 `comment_prep`/`comment_send`/`comment_verify` 无状态子命令，**一站式 `post_comment` 协议 case 删除**（单一路径防分叉）；executor `post_comment` 动作内部走 prep→send→finalize（只读轮询 6s+5s+5s，ctx/stopCh 双可中断），证据+`posted_at`+`send_error` 落 `extracted_data`（I4 续跑位点）。编排/LLM/dto 公开契约零变化。测试：Go 5 组（顺序/只读轮询/stopChFor/mergeExtract/公开契约）+ 扩展 6 项。
- **F6 余项（G15）**：① 历史压缩=滑窗 24+溢出**折叠台账**（`[已折叠 N 步: click×21 …]` 首行回喂 LLM，零 LLM 成本的 browser-use 压缩等价）② 新元素标记=SW 按 tab 基线 diff，新行 `*` 前缀（`*[index]` 语义），open_tab/navigated-click 自动清基线。
- **G9 死代码全收口**：Brain 三个旧转发壳（唯一入口=GeneratePlanReflect）/Hand.tabExists/BrowserSession 四个恒 0 字段+BrowserTask.LlmPlanID（模型删、DB 列按 D6 留）全部删除；repo 签名收窄（UpdateSnapshot/UpdateMetrics/UpdateArtifacts）；BuildLoopNudge 接线循环提示；captureVisibleTab 参数笔误修正；UI 去 Hand 延迟展示项。
- **A1–A6 真机回归全绿**（v1.4.0 扩展+user-server.r25c）：A1 三平台读链路 completed（xhs195/douyin186/xianyu187）；A2 Brain 模式 session191 目标达成+judge 独立复核+循环 nudge 生效；A3 CDP 拟人节奏；A4 create→publish→run→终态零人工；A5 command_log+finalize 证据全链可审计；A6 维持 R20 结论。

**真机暴露并修复的新缺陷**：
- **R1（P0）send 超时=结果未知 ≠ 失败**：小红书重页上 comment_send 回包可超 45s **但评论实际已提交**（session179 判死、181 起回查发现入库）。修=send 任何结局（成功/出错/超时）一律进 finalize 回查，verified 即步成功并保留 send_error 审计——Postiz「有心跳后超时=结果未知→回查，绝不重发」语义的完整落地（F2①/F2② 至此才真正闭环）。
- **R2（P0）Brain 超时后终态写回丢失**：executeBrain 因 ctx 取消收敛后，用已 Done 的 ctx 写 failed 终态被 DB 驱动连带取消→session 永久 active（session188 实证）。修=ExecuteSession 收口段统一 `context.WithoutCancel`+120s writeCtx。**教训：看门狗保证了"返回"，没保证"写回"——收敛路径上的状态持久化必须脱离被看护 ctx 的取消链**（与 R22 execCtx 不响应取消同族）。
- **R3 finalize 证据归属**：先误取评论区首条→改"含目标文本优先"→再收口"**恰等于目标文本=own 证据优先**"（防他人引用同文误判）。
- **R4 运行态纪律**：nm-host 假死新形态=进程活+host/status 在列但命令全超时；判据=轻量 wait pong 任务，不通即 pkill 让 SW 重拉。**R26-1 ✅已产品化**：host_registry 命令级超时计数（连续 2 条判假死→主动 close→复用断连清理钩子+nm-host 退避重连=自愈），SIGSTOP 注入真机全链验证。
- **R5 send 45s 慢回包本体**：**R26-2 ✅归因三分落地**——扩展定位注入 raceTimeout deadline（15s，注入未执行=零副作用），Go 侧 `*_inject_timeout_` 早返「未提交」不烧 finalize、WS 超时仍 finalize 回查（R1 语义不变）；45s 慢回包根治（CDP 事件批量化）挂下轮独立论证。

---

## 附录一：九文档合并映射（各篇处置）

| 原文档 | 原路径 | 处置 | 内容去向 |
|--------|--------|------|---------|
| CHROME_MCP_BROWSER_AUTOMATION.md | user-web/docs/ | 归档（选型起点，5 约束与方案 A 推荐） | §1.3 C1–C8 前身、§3.1 |
| BROWSER_AUTOMATION_TECH_DESIGN.md | user-web/docs/ | 归档（v1 方案，多处被 LANDING 纠错） | 手脑分离→§1.1；架构/DDL/原语以 §4 定稿为准 |
| BROWSER_AUTOMATION_PRODUCT_DESIGN.md | user-web/docs/ | 归档（v1 产品稿，DDL 被废） | 产品定位/触发三方式/边界→§1.1/1.3 |
| BROWSER_AUTOMATION_DEEP_RESEARCH.md | user-web/docs/ | 归档（部分结论被 LANDING 再修） | 帧协议/生命周期定稿→§4.2/4.6；附录 B 五实证已被实现回答 |
| BROWSER_AUTOMATION_PROJECT_LANDING.md | user-web/docs/ | 归档（v2 落地基准 A1–A8 修订） | 通信模型/DDL/多租户→§4 全部；遗留口（nginx deny）记 §5.2 附注 |
| BROWSER_AUTOMATION_TECH_DECISION.md | docs/architecture/ | 归档（v4.0：选型论证+附录 A/B/C） | §3.1/3.2/3.5、勘误 B1–B5、I1–I3→§4.7 |
| BROWSER_AUTOMATION_SOLUTION.md | docs/architecture/ | 归档（I1–I3 实施验证 v2） | §4.3/4.6/4.7 逐行吸收 |
| BROWSER_AUTOMATION_FULL_LINK.md | docs/architecture/ | 归档（S0–S8 全链路论证 v1.1） | §4.1–4.4 全部（含时序/错误码/schema/口径修正） |
| BROWSER_AUTOMATION_MODULE_TECH_PROOF.md | user-web/docs/platform-base/ | 归档（v2.0 四问调研） | 全文即本文骨架：§2/3.3/5 系列 |

**不在合并范围**（非 BROWSER_AUTOMATION_ 前缀，原位保留，被本文引用）：platform-base/ 四篇（Charter 铁律原文、DESIGN 三平台矩阵与 P1–P8、AGENT_BASE_PROOF、BRAIN_PRODUCT_SPEC）——它们是约束源与分项规格，继续独立演进；`uploads/browser_automation/`（运行时数据）不动。

## 附录二：未核验清单（不编造；二验后收窄）

**二验已升格为确证（移出本清单）**：arXiv 2507.14799（标题/机制一手确认）、Cloudflare `/accessibilityTree` 2026-07-07 changelog 一手确认、browser-use 四参数默认值官方参数页一手确认、Stagehand service-worker 执行语义（experimentalBatch 原文）。
**二验后确认无法核验（保留并标注）**：Claude in Chrome 实际输入 API（官方未公开，推断 chrome.debugger）；Chrome 147 官方 changelog（不存在对应页）；326KB/11KB、44%、93%、21ms、bu-2-0、Cloud 78% 等定量叙事（出处未找到，已从 §3.5 删除不作论据）；pptr.dev/guides/input 原页（404，结论改自 API 页+源码）；闲鱼评论接口（维持"未找到"）；CDP Runtime.enable 检测一手来源（404）；imeSetComposition 生产使用者；行为节奏量化阈值（无权威研究）；"LocalSession"名称/独立 planner 小模型（文档未见）。

> 实施排期以 §5 为准；任何对本文 §4 事实层的改动需在 §5.4 追加勘误行并同步更新本文件修订记录。

## 附录三：二轮复审记录（2026-09-11，逐项头脑风暴）

对 v1.0 每一节做了"可攻击点"头脑风暴（事实错了吗/数字有源吗/声明被消费吗/场景想全了吗），凡有实证价值的全部落为正文修订，此处仅存目：

| 节 | 攻击点 | 结果 |
|---|---|---|
| §3.5 | 附录 C 的 6 个外部定量/口号是否有源 | **4 条降格删除**（21ms、runtime 口号、bu-2-0、Cloud 78%——二查均未见于官方文档），**2 条升格确证**（arXiv IPI 论文、Cloudflare /accessibilityTree），browser-use 四参数默认值升一手 |
| §4.1/S7 | "写串行（MaxConcurrentJobs）"是已实现吗 | ❌ 零消费=声明未实现，且挖出更大的并发闸空白 → **G18/F8** |
| §4.6 | screenshot 抢焦点是否被声明 | ❌ captureVisibleTab 机制必然但 C2 例外面从未向用户声明 → **G17**（§1.3 加例外注） |
| §4.4 | 日志/plan 表会不会无限长 | ❌ 无任何保留策略 → **G19**（与 D1 同批设计） |
| §4.6 | cron 按谁的 9:00 | ❌ 服务器 Local 时区，无 time_zone 列 → **G20** |
| §4.2 | stop 是否即时 | ✅ stopCh 机制完备但步边界生效（最终一致）→ **G21**（文案口径，不改机制） |
| §5.2-G14 | judge 是否接触页面 | ❌ 喂的是自报摘要（executor.go:316），比"缺 grounding"更严重 → 表述升级+F5 改"done 前独立复核" |
| §5.2-G2 | attr 证据行号 | primitives.js 行号修正（343-349/461，dispatch 实传 cmd.attribute 而 Go 侧断链属实） |
| §4.1 | 版本锚点/时序/schema/常量速查逐条 | ✅ 与代码一致，无需改（含 bh_ 格式、四处 1.2.0、熔断数全表复核） |
| §1 | 产品边界"不做反检测"与拟真层是否矛盾 | ✅ 不矛盾：拟真=个人助理节奏拟合（低频），反检测=CAPTCHA 破解/指纹伪造（矩阵工具路线），边界表述维持 |

**二验通过、未产生改动的主要断言**：C1–C8 矩阵推理、Chrome 136 事实、NM 帧 1MiB/4GB、双看门狗、judge fail-open 拦截、Postiz 三段式描述、xiaohongshu-mcp 参数表、G1–G16 原证据链（抽查行号全部命中）。

| 版本 | 日期 | 变更 |
|------|------|------|
| v1.0 | 2026-09-11 | 九文档合并（四问框架重构；G1–G16 差距全量、勘误 B1–B10 定稿、I1–I3 收口、技术栈总表与外部对标并入） |
| v1.1 | 2026-09-11 | 二轮逐项复审：§3.5 外部论据六条重裁定（4 删 2 升）；新增 G17–G21+**F8 并发闸**（接现成 CountRunningByUser）；G14/F5 升级为"judge 喂自报摘要→done 前独立复核"；G2 行号修正；附录二收窄+附录三复审记录存目 |
| v1.2 | 2026-09-11 | **R24 实施轮**：D1–D4/F1–F8/G17–G21/T1/D5 全量代码落地（Go 模块+扩展 v1.3.0+user-web+迁移 v3.40.0）；§5 各 G 项标注实施状态与证据；G17 定稿修正（不激活=静默截错，改服务端硬闸拒 Brain 轮内 screenshot）；Charter 现状表/DESIGN 四件套口径回写（B6/B9 勘误落地）|
| v1.3 | 2026-09-12 | **R25 轮**：新增 §1.2 执行前提 P0（宿主机默认 Profile+社交账号人工已登录，凌驾 C1–C8），原 1.2–1.5 顺延为 1.3–1.6；§1.1 "告诉我账号密码"示例与 P0 冲突已修正；R25 全链路调研+优化+三平台真机评论执行结果见 §5.6 |
| v1.4 | 2026-09-13 | **R25 实施轮（扩展 v1.4.0）**：F2② 三段式拆回 Go+finalize / F6 历史压缩+新元素标记 / G9 死代码全收口 / A1–A6 真机全绿，见 §5.6；新缺陷 R1 send 超时回查、R2 终态写脱离取消链、R3 证据 own 归属全部落地；§4.6 版本锚点三处 1.4.0、协议口径 15+3 内部子命令 |
| v1.5 | 2026-09-13 | **R26 运行态产品化轮（扩展 v1.4.1）**：R4 假死自愈探针产品化（host_registry 命令级超时计数连续 2 判死主动断开复用钩子，SIGSTOP 真机全链验证）；R26-2 注入竞速 deadline+Go 归因三分（inject_timeout 早返零副作用/WS 超时回查/业务错误正常失败）；42 项 vitest+Go 全包全绿；版本锚点三处 1.4.1；详见 R25 审计文档 §5 |
| v1.6 | 2026-09-14 | **R27 双项（扩展/nm-host v1.4.2）**：① I5 审计导出 GET /sessions/:id/export（会话+步+命令流+LLM 成本账单请求归并、snapshot 大文本不带出、真机 195/188 验证）+前端导出按钮；② **探针自愈链真机补全**（更正 v1.5 过于乐观的"全链验证"结论：close-only 后 nm-host 退避重连成僵尸注册、服务不恢复，session199/204-208 四轮实证）——判病先发 `__host_shutdown__` 控制帧令 host 进程退出→Chrome 重拉全新进程，端到端零人工自愈在 v1.4.2 真机闭环（僵尸 59920 退出→新 pid 60643 注册→pong completed）。版本锚点三处 1.4.2 |
