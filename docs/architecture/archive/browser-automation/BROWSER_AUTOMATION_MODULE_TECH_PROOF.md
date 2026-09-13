# 浏览器自动化模块 — 四问调研与技术方案论证

> 文档版本：v2.0 ｜ 2026-09-11 ｜ 项目：hivemtk ｜ 性质：需求→问题→同类→差距 四问框架的调研论证（重构自 v1.0 三源交叉版，v1 的 G1–G9 差距全部保留并归入对应问题）
> 调研方法：三路业界同类深度调研（输入拟真/风控对抗、Agent 循环内核/可靠性工程、发布可靠性契约，均源码/官方文档一手）+ 本模块代码逐文件复验（关键新发现 G10/G11 为亲手 grep + 通读函数体实证）。
> 纪律：每条结论带出处（文件:行号 或 URL + 置信度）；未核验显式标注。

---

## 问题一：我们的要求是什么

需求有三层权威来源，优先级从高到低：

1. **Charter 四铁律**（PLATFORM_BASE_CHARTER.md，2026-09-09 拍板，冲突最高裁决）：
   - 铁律1 三平台一视同仁，差异只存在于 L3 适配器，能力不吹不实现；
   - 铁律2 **UI 自动化、模拟人工：写操作必须产生 isTrusted=true 的输入（CDP），执行节奏模拟人工，检测即检测页面真实状态**；
   - 铁律3 基座=能力供应方，LLM(Brain)=唯一智能来源，新场景=新目标/新平台=新适配器/新原语=扩一枚举；
   - 铁律4 全程全自动 + 失败可归因 + 可审计（command_log）+ 可自愈，零人工介入。
   - 验收标准 A1–A6 逐条可测。
2. **硬约束 C1–C8**（TECH_DECISION.md §1）：复用主 Chrome Profile、后台 tab 不抢焦点、重启自动连接无弹窗、多 Agent 共会话、不引入独立浏览器实例、统一端口 :8204、Go 技术栈、私域单租户。→ 推论：**只能用"寄生真实浏览器"通道，Playwright/Puppeteer/CDP 直连/Vision 全部出局或降级**（选型已定，不再翻案）。
3. **产品定位**（DESIGN §5.7）：个人助理工具——单账号、低频、用户自己的账号；**不做批量矩阵设计**。这条决定了"风控对抗"的目标不是绕过，而是**行为像本人 + 失败如实报告**。

**需求的本质一句话**：在用户自己的 Chrome 里、用他的登录态、由 LLM 编排、全自动地替他在小红书/抖音/闲鱼上完成"读+写"任务，每一步可信（对平台像真人）、可控（对基座有契约）、可查（对人有审计）。

---

## 问题二：我们要解决什么问题

把需求拆成 7 个必须解决的技术问题（本模块的真问题域）：

| # | 问题 | 为什么在我们的约束下难 |
|---|------|----------------------|
| Q1 | **可信输入**：让 SPA 框架（React/Vue）和平台把程序化操作当真 | CDP 直连被禁（C1+C2），唯一合法通道=扩展内 chrome.debugger；dispatchEvent 恒 isTrusted=false 且 React 忽略（R17 实测） |
| Q2 | **行为拟真**：键鼠节奏/轨迹接近真人，降低机器特征 | 单账号定位下唯一防线就是拟真；量化阈值无权威研究（DESIGN §5.6 未找到） |
| Q3 | **页面理解**：把任意页面喂给 LLM，省 token 且不失真 | 快照体积、动态 DOM、视口内外裁剪、历史步的表示衰减 |
| Q4 | **LLM 决策可靠性**：规划→执行→验收→恢复的循环不空转、不自欺、不烧钱 | LLM 幻觉参数、假 done、原地打转、成本失控 |
| Q5 | **写操作成功判定与防重**：平台会静默吞（小红书），网络超时重试会**双发** | "点了发布"≠"发布成功"≠"可安全再点一次"——三件事各自独立 |
| Q6 | **执行链可靠性**：长连接生命周期、断连归因、失败恢复、审计闭环 | MV3 SW 生命周期 + NM 假死 + 半开 TCP 僵尸连接 + 进程重启丢定时器 |
| Q7 | **平台异构与知识收口**：三平台通道不同（DOM/API/未知），基座必须零平台知识 | 能力矩阵诚实性（声明=可用）；签名协议层与行为层是两条独立的线 |

---

## 问题三：同类是如何解决的（技术方案明细）

> 每条：机制 → 参数/序列 → 来源 → 置信度。这是本轮调研的主体，也是 Q4 差距表的对照基线。

### Q1 可信输入

- **Puppeteer**：keyboard.type 逐字符分流——ASCII 键走 `Input.dispatchKeyEvent`（keyDown 带 text/code/windowsVirtualKeyCode + keyUp），**非按键表字符（中文）走 `Input.insertText`**（不产生 keydown/keyup，浏览器进程注入故 isTrusted=true）；mouse.click = `mouseMoved→mousePressed{buttons:1,clickCount}→delay→mouseReleased{buttons:0}`。来源 pptr API 文档 + cdp/Input.ts 源码（一手）。
- **Playwright**：同 CDP 底座；API 分工明确——`fill()` 一次写入触发 input；`pressSequentially()` 逐字符完整键盘事件；`insertText()` 只触发 input。`dispatchEvent('click')` 被官方定性为"simulate by any means possible"=逃生口非正路。来源 playwright.dev/docs/input、/docs/actionability（一手）。
- **browser-use**：动作看门狗 `default_action_watchdog.py`（3752 行，一手源码）：点击=CDP `dispatchMouseEvent` 三步 + `_check_element_occlusion`（DOM.resolveNode+Runtime.callFunctionOn hit-test）+ scrollIntoViewIfNeeded；输入=逐字符 keyDown→sleep 5ms→**char 事件（带 text，注释"crucial for text input"）**→keyUp，字符间 sleep 1ms，大写/符号自动 modifiers=8；输入完成后 `Runtime.callFunctionOn` 补发 input/change/blur 并定向触发 React fiber/Vue（**JS 合成补发是"兜底"，主通道仍 CDP**）。⚠️ 其源码里有一行 `Object.defineProperty(ev,'isTrusted',{value:true})`——JS 层无法覆盖原生 isTrusted，该行无效，反证"合成事件伪造可信"走不通。
- **Midscene（Chrome 扩展 bridge 模式）**：`chrome.debugger.attach(tabId,'1.3')` 懒挂载，动作经 `Input.dispatchMouseEvent/KeyEvent/insertText` 发送；其 `cdpInput.ts` 文件头自注 **"From puppeteer cdp/Input.ts with modifications"**——扩展形态与我们的路线完全同构，业界同型已验证。输入策略三档 `legacy|sequential|bulk`，bulk=一次 insertText 且与 delay 互斥。来源 midscene 源码（一手）。
- **xiaohongshu-mcp（同场景最强参照，15.7k★，Go+rod）**：输入=Focus→WaitEnabled→WaitWritable→**逐码点 `page.InsertText`（CDP）+ 每字符 LogNormal(30~400ms) 延时**——不产生 keydown/keyup，因为小红书评论框只认 input 事件。来源 comment_feed.go/humanize 包（一手源码）。
- **Anthropic browser use tool**：官方把"截图+坐标的 computer use"与"a11y tree+ref 的 browser use"分成两个工具集；执行器全部由本地驱动层实现（"nothing runs on Anthropic's side"）——模型决策+本地执行的分工与我们的 Brain/Hand 分层同构。产品级参照的输入通道未公开（推断 chrome.debugger）。来源 docs.claude.com（权限模型一手，通道未核验）。

**同类共识**：写操作一律走 CDP 级注入；JS 合成事件只做框架兼容性补发或逃生口；中文统一 insertText。

### Q2 行为拟真

- **xiaohongshu-mcp humanize 包（目前能抓到的最完整参数表，一手）**：
  - 延时模型 **LogNormal 采样**，11 个动作相位：Keystroke 30–400ms、ClickHold 45–250ms、BeforeClick 80ms–1s、PointerSettle 200–1200ms、AfterNavigate 600ms–6s、AfterInteract 2–3s…；
  - 鼠标：**三次贝塞尔曲线轨迹**（步数=距离/10、夹 10–40 步、每步 5–9ms、控制点垂直偏移 ±5–15%、easeInOut）+ **落点抖动**（元素 15% 尺寸与 8px 取小的均匀偏移）+ 点击前 hit-test。
- **Puppeteer/Playwright**：delay/steps 参数提供机械拟真（默认 0=不拟真——**测试框架不做拟真，生产自动化才做**，这个分野本身是论据）。
- **browser-use**：固定微延时（5ms/1ms）只求框架兼容，**无轨迹无分布采样**——其 stealth 归 Cloud 付费产品，开源件不含。
- **量化阈值**：所有项目均无"多少 ms 像人"的权威依据（DESIGN §5.6"未找到"维持）→ 拟真参数只能抄实测有效的同场景项目（xiaohongshu-mcp）。

### Q3 页面理解

- **browser-use**：交互元素序列化为 `[index]<tag/>` 树文本；`*[index]` 标记**上一步后新出现的元素**；`|SCROLL|`/`|SHADOW|` 前缀；默认只列视口内；可交互文本截断 40k 字符；截图为 GROUND TRUTH，历史步截图降级为 **4px 占位图**（省 token 的核心手法）；system+state 消息 `cache=True` 保 prompt cache（step_info 刻意放消息尾部）。
- **playwright-mcp（Microsoft）**：a11y 树 YAML 快照 + `e<n>` ref（`aria-ref=e12` 解析回元素；iframe 前缀 `f1e3`）；**每个动作响应自动附带新快照**（动作后 500ms settle）使 ref 与页面同步演进；ref 失效=硬错误"Try capturing new snapshot"（无自愈，恢复成本转移给下一轮理解）；坐标/视觉仅 `--caps=vision` opt-in。官方立场原文：MCP 适合"需要持久状态、富内省、对页面结构迭代推理的**专门 agent 循环——探索式自动化、self-healing、长时自主工作流**"。
- **我们的 @e{N} refs + MAX_NODES=400 + 隐藏过滤**：形态与两者同构（AGENT_BASE_PROOF §1.2 结论维持），差距在下游——无"新元素标记"、无截图降级、无 settle 时序约定。

### Q4 LLM 决策可靠性（循环内核）

- **browser-use 协议全貌（一手源码 views.py/prompts.py）**：
  - 输出 schema required=[evaluation_previous_goal, memory, next_goal, action]，action 永不为空；`flash_mode` 砍掉自评只留 memory+action；
  - **历史管理**：滚动窗口"首条+省略标记+最近 N"；每 25 步且全文超 40k 字符触发**小模型压缩**成 ≤6k 字符 `<compacted_memory>`，压缩提示词明令"未见显式成功确认不得标 completed，注入时附 unverified 警告"；
  - **一步多动作**：max_actions_per_step=5，页面一旦变化剩余动作自动截断（page-changing 动作必须置尾）；
  - **失败治理**：max_failures=5 连续失败上限；`final_response_after_failure=True` 超限后强制一次收尾调用产出部分结果；fallback_llm 主模型耗尽退避后整局切备用；step_timeout=180s；
  - **循环检测三档**：动作归一化哈希（search 按 token 排序、click 按元素文本）20 动作窗口内重复 5/8/12 次分三档注入"换思路"；页面指纹（url+元素数+DOM SHA）连续 5 步不变=停滞提示；规划器 stall@3 连败注入 replan；
  - **done 前验收**：`<pre_done_verification>` 六条——重读需求逐条对照、**每个输出值必须原样出现在工具输出中（禁止先验知识补洞）**、阻塞错误必须 success=false、"部分结果+false 胜过谎报成功"；is_successful() 只是自评，文档明言"重要外部动作须对目标系统独立核验"。
- **Stagehand（确定性优先的另一极）**：act/observe/extract 三原语；**selfHeal=true 默认**——缓存 selector 不再解析→回炉 LLM 重定位（语义=缓存失效治愈，非通用重试）；恢复模式四级（observe 重验→指数退避→agent fallback 补多步→清缓存重探索）；服务端 action cache key=instruction+a11y 树+options+URL（variables 用 `%name%` 占位，key 不含值→一次 priming 永久命中）。
- **Midscene**：replanningCycleLimit=20（UI-TARS 40）内置重规划上限；plan 缓存失败→当次回退 AI 规划 + **清空 stale flow、不回写脏计划**；查询类结果永不缓存；CLI `--retry/--continue-on-error/--concurrent` 批跑容错；每 run 自包含 HTML report（Plan/Locate 状态+耗时+截图，可 split 成 JSON）。
- **playwright-mcp 的对照提醒**：编码类 agent 官方更推荐 CLI+SKILLs（省 token），MCP 留给专门的 agent 循环——**Brain 循环与确定性编排两条腿各有归属**，与我们 API/Brain 双模式并存的设计互证。

### Q5 写操作成功判定与防重（本问题的标杆是 Postiz）

- **Postiz 三段式契约（一手源码 social.integrations.interface.ts + Temporal workflow）**：
  - `post()→{status:pending, pendingData}`；`checkPostStatus()→pending|ready|completed`；finalize 执行剩余变更；**pendingData 是不透明 provider 私有状态，泛型代码绝不解读**；
  - **防双发核心契约（源码注释即规范）**：finalize 的变更真正落地后，checkPostStatus 必须返回 completed、**永不得再返回 ready**（否则 finalize 在"结果未知"失败后重试=重复发帖）；唯一豁免=变更幂等；
  - **读写分治重试**：checkPostStatus 只读→自动重试 3 次/10s 间隔（"重试永不可能重复发帖"）；postPending/finalize 不可逆→**maximumAttempts:1 禁自动重试** + 30min 超时 + 15s 心跳/3min heartbeatTimeout——心跳判因矩阵：**无心跳=worker 从未启动=可安全重试；有心跳后超时=结果未知→markUnconfirmed 通知用户"可能已发布，先去账号检查再重发"**；
  - finalize 前先持久化本次 check 的 pendingData（finalize 死在中途时，下轮 check 能看出已授权了什么）。
- **xiaohongshu-mcp（同平台直接参照）**：提交后 `waitCommentRendered(page, content, 4s)`——每 300ms 轮询 `.comments-container` innerText 是否含评论文本，未渲染判失败且错误文案明写"可能账号被限制"（防假成功）；README 运营约束（单账号禁多网页端同登、日发帖上限实测约 50、先查违禁词）。
- **DBOS（断点续跑原理参照）**：每个 step 返回值 checkpoint 进 Postgres，崩溃后从最后已完成 step 恢复重放、已完成步骤跳过不重执行；成本=每 step 一次 DB 写。→ "推理记忆留在 LLM 层，外部事实判定交给 durable 层"是当前趋势分层。
- **MediaCrawler/xhshow（协议层参照）**：签名的输入=a1 cookie+uri+参数，会话状态=固定页面加载时间戳+单调递增计数器（SessionManager，自述效果待验证）；小红书主方案已从"浏览器上下文取签名"迁到 xhshow 纯算，抖音 a_bogus 仍绑真实页面上下文——**平台间策略分裂，无统一方案**；461/471→读 Verifytype 头抛验证码异常（不绕过直接失败）、300012=IP 封换代理、300011=账号限制不重试——**错误分类驱动的处置矩阵**。

### Q6 执行链可靠性

- browser-use：1/2/4s 重连 watchdog（cdp-use 直连场景）；Midscene 扩展：already-attached 容忍 + detach 类错误重 attach 重试 2 次；Claude for Chrome：prompt injection 分类器 + 高风险动作（发布/购买）强制用户确认——**产品级 Anthropic 也在"全自动"上保留了发布确认一环**（与我们铁律 4 的张力见 D7，需拍板）。
- Playwright actionability 五项（visible/stable/enabled/editable/receives-events）已被 xiaohongshu-mcp（WaitInteractable/ensureClickable）与 browser-use（occlusion check）各自重新实现——**裸 CDP 缺这一层是业界公认刚需，不补就是拿坐标赌**。

### Q7 平台异构

- 同类分层同我们 Charter：通道知识收口 provider/adapter（Postiz 30+ provider、Mixpost SocialProviderManager）；能力声明+optional 方法（Postiz optional 语义）；契约默认失败（CommentLocatorsFor fails-loudly 已实现）。
- 写链路通道实测三分：小红书 DOM 可行（15.7k★ 一年+标杆）、抖音公开 API 路线（a_bogus 版本迭代风险，2024-09 大变更先例）、闲鱼接口未公开（真机抓包前置）。

---

## 问题四：对照我们的代码，缺哪些、哪些要改

### 4.1 逐问题对表

| 问题 | 我们的现状（证据） | 同类基线 | 判定 |
|------|------------------|---------|------|
| Q1 可信输入 | **仅 post_comment 走 CDP trusted**（primitives.js:429-452→cdp/input.js typeText/clickAt）；**普通 click/type/click_near 全部 JS 合成**：injClick=`el.click()`+dispatchEvent(PointerEvent/MouseEvent)（primitives.js:9-38），injType=execCommand/dispatchEvent(InputEvent)（primitives.js:40-101） | 所有同类：写操作主通道 CDP，合成仅兜底 | ❌ **G10 新 P0**（见下） |
| Q2 拟真 | 步间 ±30% 均匀抖动（executor.go:723）；CDP 内对数正态键入/点击 hold/pointerSettle（cdp/input.js:35-53,136-162）；轨迹=**起点固定偏移 (x-20,y-45) 的 5 步线性插值**（input.js:140-148）；无 hit-test 遮挡检测、无落点抖动 | xiaohongshu-mcp：贝塞尔 10–40 步+easeInOut+落点抖动+点击前 hit-test；Playwright 五项 actionability | ⚠️ G12/G13 |
| Q3 页面理解 | @e{N} refs + MAX_NODES=400 + 隐藏过滤（accessibility.js）；快照原样进 prompt（已有 48k rune 预算截断） | browser-use：新元素 `*[` 标记、视口裁剪、历史截图 4px 降级、prompt cache 布局 | ⚠️ 够用，记三项低成本增强（E 组） |
| Q4 决策可靠性 | reflect 四字段+跨轮回喂+循环指纹（连续 3 轮相同→nudge）+JudgeDone+连续 2 次 fail-open 拦截；token 预算 200k 熔断；参数 clamp；LLM 错误分类重试；双看门狗 | browser-use：三档循环检测（5/8/12）、压缩记忆（25 步/40k→6k）、一步 5 动作截断、pre_done_verification 六条、fallback_llm、收尾调用 | ✅ 骨架达标；⚠️ G14/G15 细节差 |
| Q5 写判定与防重 | 三段式在扩展端融合实现、未验证抛错（fails-loudly）；**但 Go 步级重试对 post_comment 同样生效**（executeStepWithRetry 对所有 action，executor.go:453+，LLM 可下发 retry_count≤3）；verify 通过仅回 {posted,verified}，无 finalize 落 commentId | Postiz：不可逆变更 maximumAttempts=1、读写分治、心跳判因矩阵、completed 永不回 ready；xiaohongshu-mcp：4s 轮询渲染回读 | ❌ **G11 新 P0（双发风险）**、G3 记账半截 |
| Q6 执行链 | append-only command_log（**有写无读 G1**）；断连清理钩子+per-user 路由+双 token（常量时间比较+reset API，I2 实已完成）；重试定时器内存态（G5）；WS 零心跳（G4）；Host 帧 1MiB/4GB | 业界重连/watchdog 标配；心跳=长连接刚需 | ⚠️ G1/G4/G5 维持 |
| Q7 平台收口 | 接口 6 方法+CommentPoster 可选+注册表+能力矩阵+fails-loudly；三适配器纯静态；Brain 平台知识注入已接（brain_prompts.go:91-107） | Postiz/Mixpost 同构 | ✅；Recipe/Signature 空口不建（G6 裁定维持）；平台错误处置矩阵可对标 MediaCrawler 细化（retry 白名单类 vs 不重试类） |

### 4.2 新发现详述（本轮调研的核心增量）

**G10（P0）｜铁律 2 只对 1/4 的写原语成立——click/type/click_near 仍在用 R17 已证伪的通道**
- 事实（本会话通读 primitives.js:9-101 实证）：`click` 走 `el.click()` + 合成 PointerEvent/MouseEvent（isTrusted=false）；`type` 走 focus+execCommand('insertText')/合成 InputEvent；`click_near` 同合成序列；只有 `post_comment` 内部融合 CDP 通道。
- 论证：R17 当时的实测结论就是"React 受控组件忽略合成事件"，修复只落在了 post_comment 一条路径上，**普通原语没有回收**（记忆 hivemtk-browser-automation-domain 记录的正是这一史）。Charter 铁律 2 写的是"写操作必须产生 isTrusted=true"，不是"发评论必须"。后果链：Brain 编排用 click/type 操作小红书评论框之外的受控组件（点赞/关注/表单）→"返回 ok 但页面没反应"=假成功→检测不到→LLM 基于假回包继续规划。这是**声明与事实不符**在输入层的最大一处，且违反的恰是最高优先级铁律。
- 改进（F1）：把 post_comment 里已验证的 CDP 通道抽成通用能力——`click` 加可选 `trusted:true` 语义（injClick 定位拿坐标→cdpInput.clickAt）、`type` 的 contenteditable/input 分支改走 cdpInput.typeText（逐码点 insertText，xiaohongshu-mcp/我们 post_comment 双先例背书）；执行器默认对 click/type 开启 trusted 模式（我们场景全是 SPA），保留 DOM 兜底（页面可访问性差时）。同类 browser-use 的"CDP 主+合成补发框架事件"顺序也印证：主通道必须是 CDP。

**G11（P0）｜post_comment 可被步级重试二次提交——业界唯一写成接口级契约的红线（Postiz）我们反向违反**
- 事实：executeStepWithRetry 对任意 action 按 retry_count（clamp≤3）重试（executor.go:453-461）；post_comment 的失败形态包括"CDP 键入成功+点发布成功+verify 超时未渲染"（R17/R19-6 实测的静默吞正是此态）——**此时结果未知，重试=可能双发**；网络半断（命令已执行、回包丢失）同理。
- 同类基线：Postiz 对不可逆变更 maximumAttempts:1 + 心跳判因（无心跳=没执行过可安全重试；有心跳后超时=结果未知转人工确认）；"completed 后永不回 ready"防双发。这不是 Postiz 一家偏好——重复发布在社交场景是用户可直接感知的事故。
- 改进（F2，两步走）：① 即刻：Go 侧对 `post_comment`（及未来一切写原语）**强制 retries=0**（服务端钳位，不信任 LLM/编排参数），失败即归因终止——与铁律 4"可归因"一致且改动 ~3 行；② 正确版：三段式拆分回 Go 编排（pending→verify 轮询由后端发起，verify 只读可重试，提交不可逆不重试）+ verify 通过则 finalize 落 commentId/时间戳（顺带闭环 G3 语义与 DESIGN:81-84 的 finalize 欠账）。

**G12（P1）｜鼠标轨迹是 5 步线性+固定起点，拟真度低于同场景标杆一档**
- 事实：cdp/input.js:140-148 轨迹从 `(x-20,y-45)` 直线插值 5 点、每步 8ms；起点恒定可被轨迹形状统计识别（真人不会每次从同偏移出发）。
- 同类基线：xiaohongshu-mcp 三次贝塞尔（步数随距离 10–40、每步 5–9ms、控制点随机垂直偏移 ±5–15%、easeInOut）+落点抖动（15%/8px 取小）；xiaohongshu-skills 也是 5 步模式但那是"够用"下限。
- 改进（F3）：贝塞尔插值 + 随机起点（自上一 mousemove 位置，无则随机屏内点）+ 落点抖动，全部参数照抄 xiaohongshu-mcp 实测值；纯扩展侧改动，可单测断言"两次调用轨迹点序列不同"。

**G13（P1）｜写操作前无 actionability 检查——拿坐标赌，赌输就是假成功/误点**
- 事实：injClick 无 visible/stable/enabled/receives-events 检查（只 scrollIntoView）；cdp clickAt 直接按坐标，弹层/遮挡下会点到浮层。
- 同类基线：Playwright 五项定义是事实标准；browser-use occlusion check、xiaohongshu-mcp WaitInteractable+hit-test 都自建了这层——**裸 CDP 缺 actionability 是业界公认刚需**。
- 改进（F4）：扩展内 `ensureInteractable(el)` 统一前置：可见（boundingBox 非零+visibility）→ 稳定（两帧同 bbox）→ enabled/editable（按动作分，对齐 Playwright 表）→ elementFromPoint 命中自身；不满足则滚入视口重试→超时抛 element_not_interactable（错误进 ClassifyError=retry 类）。这是 G10 改造的顺带件（trusted 路径需要坐标，坐标必须先证明可点）。

**G14（P1）｜Brain 的 done 验收没有"数据 grounding"条——judge 可被 LLM 自述骗过**
- 事实：JudgeDone prompt（brain_prompts.go buildJudgePrompt）输入=goal+最终摘要/extract；browser-use 的 pre_done_verification 核心是"**每个输出值必须原样出现在工具输出中，禁止先验知识补洞**"+ "部分结果+false 胜过谎报成功"。
- 改进（F5）：judge prompt 注入 command_log 中真实 extract/result 片段作为唯一证据源 + 显式要求逐值比对；done 的 extract 字段与落库 result 做程序化包含校验（零 LLM 成本，先拦明显幻觉）。

**G15（P2）｜无历史压缩、无新元素标记、无一步多动作截断**
- browser-use 三项机制我们缺：>24 轮后 history 仍平铺（无小模型压缩→长任务 prompt 膨胀）；快照无 `*[N]` 新元素标记（LLM 难感知动态内容）；steps 顺序执行无"页面变化即截断剩余"（一次 5 动作中若第 2 个导航，后 3 个打在旧 DOM）。
- 改进（F6，低成本优先）：先做**动作后页面指纹变化→截断本轮剩余 steps**（executor 一行判断，防错位执行）；新元素标记次之（快照 diff 打标）；历史压缩最后（触发阈值照抄 25 步/40k）。

**G16（P2）｜重试语义未分类——读操作可重试、写操作禁试、认证类快败的三线表没建**
- MediaCrawler 先例：IPBlock 换代理重试、NoteNotFound 不重试、验证码直接失败上抛；Postiz：只读重试 3 次/变更 attempts=1。我们 ClassifyError 四分类已有、但**执行层没有按错误类型决定可重试性**（detectBlockedIfFatal 只看 disconnect；步重试对所有错误一视同仁）。
- 改进（F7）：dispatchStep 失败路径接 ClassifyError：bad_body→0 重试快败；refresh_token→上抛终止会话（单账号无法刷新）；retry→按步 retry_count；disconnect→终止。与 F2 同文件同批做。

### 4.3 v1.0 遗留差距（维持有效，简列）

G1 命令日志有写无读（P0，D1 方案不变）｜G2 query attr 三层断链（P0，贯通 ~10 行）｜G3 v3.40 迁移不存在/成本账未闭环（P1）｜G4 WS 零心跳（P1）｜G5 重试定时器重启丢（P1）｜G6 四件套文档口径（改文档不改代码）｜G7 三平台写链路如实维持｜G8 测试盲区补测顺序（HostRegistry→dispatchStep→平台纯函数→cdp/input.js——**F1/F3/F4 全改扩展侧，input.js 单测从 P2 提到 P0 前置**）｜G9 死代码清单。

### 4.4 文档勘误（v1.0 §4/§5 全部维持）

TECH_DECISION 六处（bridge≠自动化扩展、I2 已完成、原语口径 15、Chrome147 无一手源降级、star 数过时、P1-1 半截）+ Charter 现状表刷新（注意：本轮 G10 发现说明 Charter 铁律 2 状态应记为 **⚠️ 部分达成**而非"✅ 已有 CDP trusted"——v1.0 §2-3/4 的表述过宽，一并修正）+ DEEP_RESEARCH 帧上限回写。

---

## 决策建议（按四问收敛）

| # | 动作 | 归属 | 优先级 |
|---|------|------|--------|
| F1 | click/type/click_near 升级 CDP trusted 主通道（DOM 兜底保留） | Q1/铁律2 | **P0 立即** |
| F2 | 写原语服务端强制 retries=0（即刻）+ 三段式拆分回 Go + finalize 落库（正确版，顺带闭环 G3） | Q5/铁律4 | **P0 立即（①）→ P1（②）** |
| D1 | 命令日志 GET + session 详情命令流面板 | Q6/审计 | P0 |
| D2 | query attr 贯通 | Q7/诚实契约 | P0 |
| F3 | 贝塞尔轨迹+随机起点+落点抖动（参数抄 xiaohongshu-mcp） | Q2/铁律2 | P1 |
| F4 | ensureInteractable（Playwright 五项对齐） | Q1/铁律2 | P1（与 F1 同批） |
| D4 | WS 心跳 + 重试持久化 | Q6 | P1 |
| F5 | judge 数据 grounding + 程序化包含校验 | Q4/铁律4 | P1 |
| F6 | 页面变化截断剩余 steps → 新元素标记 → 历史压缩（按此序） | Q3/Q4 | P1→P2 |
| F7 | 重试语义按 ClassifyError 分线 | Q4/Q6 | P2 |
| D5 | 补测：input.js 事件序列单测**提到 F1/F3 前置**；HostRegistry/dispatchStep/平台纯函数次之 | 护栏 | 随 F1 |
| D6 | 文档回写（DESIGN/Charter/DECISION/DEEP_RESEARCH）；**新增待拍板 D7**：Anthropic 产品级在发布类高风险动作保留了用户确认环——我们铁律 4"零人工"是否对"发布/评论"类写操作开一个可选确认开关（默认关，合规敏感用户开） | 治理 | P2 + 待拍板 |

**明确不做（引用同类证据）**：JS 伪造 isTrusted（browser-use 源码反证 + MDN）；合成事件替代 CDP 主通道（全部同类）；批量矩阵/多账号池（产品定位 + xiaohongshu-mcp"同账号禁多端登录"实证约束）；预建 Recipe/Signature 空接口（G6）；tiktoken；视觉通道（等 DOM 失败率数据）。

## 未核验清单（新增部分）

Claude in Chrome 实际输入 API（官方未公开，推断 chrome.debugger）；browser-harness 与 NM 的正式对比文档（不存在，其机制=chrome://inspect 复选框一次性开启）；browser-use 326KB/11KB、Stagehand 44%、agent-browser 93% 数字一手出处（未找到，仅定性采信"退潮换直连 CDP"）；pptr.dev/guides/input 原页已 404（结论改自 API 页+源码）。
