# 浏览器自动化多平台基座设计（v2 实施稿）

> 状态：**设计稿 v2（吸收二轮补调研），待确认后实施**。
> 依据：五路调研（trusted 输入 / 基座先例 / 三平台实测 / CDP 实现级 / 差距盘点），全部结论带来源，查不到的明确标注「未找到」。
> 日期：2026-09-09。目标：小红书已验证的「寄生式扩展」架构升级为通用基座，下一批平台：抖音（网页版）、闲鱼（goofish.com）。

---

## 0. 调研得出的第一性原则（基座不可妥协项）

| # | 原则 | 依据 |
|---|------|------|
| P1 | **寄生在带登录态的真实浏览器里，不做裸自动化浏览器** | 实测：三平台对无头/自动化浏览器全部拦截（小红书 302→IP 风险页 error_code=300012；抖音验证码中间页 rmc.bytedance.com；闲鱼「非法访问」弹层）。MediaCrawler/ai-goofish-monitor 等成熟项目全部走"真实浏览器 + 登录态"路线 |
| P2 | **写操作（发评论）必须产生 isTrusted=true 的输入** | MDN：dispatchEvent 恒为 false 不可伪造；React 受控组件忽略非 trusted 事件（R17 实测）。唯一扩展内合法通道 = chrome.debugger CDP Input |
| P3 | **CDP 事件序列必须完全对齐 Puppeteer 规范** | 调研：keyDown 必须带 text/unmodifiedText/key/code/windowsVirtualKeyCode；Enter=text:'\r'；鼠标必须 mouseMoved→press{clickCount:1,buttons:1}→release{buttons:0}。任何缺项都"显示成功但框架不认" |
| P4 | **每个写操作强制配对「发布后验证」** | xiaohongshu-mcp（15.7k★）waitCommentRendered：提交后 4s 内评论渲染进 .comments-container 才算成功；Postiz pending→checkPostStatus→finalize 三段式（契约默认失败 fails-loudly） |
| P5 | **平台差异收进适配器四件套，基座零改动** | Postiz 30+ provider 注册表 / Mixpost SocialProviderManager：新平台=一个目录的 locators+recipes+verifiers+signature |
| P6 | **账号是一等实体**：healthy/suspect/retired 三态 + markGood/markBad/retire 语义 | Apify SessionPool 错误分类学；小红书会静默吞评论（xiaohongshu-mcp 失败信息明确提示"账号被限制"） |
| P7 | **同一账号的会话状态单调演进**（时间戳+计数器），禁止跨请求重置 | xhshow SessionManager；Midscene 高敏场景禁用规划缓存 |
| P8 | 执行会话落库升级为 **append-only 命令-事件日志**，断点续跑=重放 | Temporal durable execution 单机版；与现有审计需求重合 |

## 1. 三平台能力矩阵（实测+开源证据）

| 能力 | 小红书 | 抖音网页版 | 闲鱼 |
|------|--------|-----------|------|
| 详情页 URL | `/explore/{noteId}?xsec_token=`（token 必须从列表透传，非登录态） | `/video/{aweme_id}` | `/item?id={item_id}` |
| 读内容 | 游客可 SSR（noteDetailMap），搜索需 X-S 签名 | 全 CSR+jsvm 混淆，需 a_bogus+msToken | 全 CSR，mtop（sign=MD5(token&t&appKey&data)，appKey=34839810，最弱但环境检测狠） |
| 读评论 | API `/api/sns/web/v2/comment/page`（需签名） | `/aweme/v1/web/comment/list/`（需签名） | 混在 `mtop.taobao.idle.pc.detail`，独立接口未公开 |
| **发评论通道** | **DOM 路线（成熟）**：contenteditable `p.content-input` + `button.submit`，15.7k★ 项目一年+不封号 | **API 路线（公开）**：`POST /aweme/v1/web/comment/publish`（a_bogus 签名）；DOM 选择器无公开资料 | **未评级**：评论发布接口无公开资料；成熟生态走 WebSocket 私信（XianYuApis 逆向） |
| 风控强度 | 搜索高/浏览中/评论中 | 全线高 | 签名弱但指纹检测狠；评论未知 |
| 461/验证码 | 461/471+Verifytype 头 | verifycenter captcha iframe | baxia 滑块+RGV587 |

**结论**：发评论的"trusted 输入"需求是小红书特有的（DOM 路线）；抖音走签名 API 不需要 DOM 输入；闲鱼连接口都未公开。**基座必须同时容纳「DOM 输入通道」和「签名 API 通道」两种发帖模式。**

## 2. 基座分层架构

```
┌─────────────────────────────────────────────────────────────┐
│ L4 编排层（已有，改造）                                       │
│  Task/Cron/Workflow + Brain(LLM) —— Brain 只调 L3，禁持平台知识 │
├─────────────────────────────────────────────────────────────┤
│ L3 平台适配层（新增，核心）—— 每平台一个目录四件套              │
│  LocatorRepo（选择器+AI 描述表，含"易变字段走 AI"声明）          │
│  Recipe（组合原语的有状态流程：search/open_detail/post_comment） │
│  Verifier（断言集合：发布后验证/拦截页识别，独立于动作层）        │
│  Signature（签名器：DOM-eval 或 纯算 或 不需要）                │
│  注册表注册：identifier + Capabilities + MaxConcurrentJobs      │
├─────────────────────────────────────────────────────────────┤
│ L2 动作执行层（已有，改造）                                    │
│  原语集分三类：                                                │
│   · 即时原子：goto/click/type/scroll/extract/snapshot          │
│     （补 actionability 检查：visible/stable/enabled/receives-  │
│       events + 严格唯一性，对齐 Playwright 语义）              │
│   · 规划式：aiAct（LLM 每轮计划，Midscene 语义）                │
│   · 洞察类：assert/query/wait_for（新增，断言独立成层）         │
│  trusted 输入通道（chrome.debugger CDP，参数表对齐 Puppeteer）  │
│  人性化层（新增）：humanize type/click（逐字符随机延时+轨迹+抖动）│
├─────────────────────────────────────────────────────────────┤
│ L1 驱动层（已有）                                             │
│  MV3 扩展（chrome.debugger/nativeMessaging/tabs/scripting）    │
│  NM Host + per-user WS 长连接 + Go 后端                         │
│  （Driver 接口保持抽象：未来可换 CDP 直连/系统级注入扩展）        │
├─────────────────────────────────────────────────────────────┤
│ L0 账号与会话层（新增）                                        │
│  Account 实体：cookie集+指纹+错误分+状态机+最近使用              │
│  markGood/markBad/retire + DetectBlock 钩子（平台判据进适配器）  │
│  错误四分类协议：refresh-token / bad-body / retry / disconnect  │
└─────────────────────────────────────────────────────────────┘
```

## 3. 关键设计决策（含理由与来源）

### 3.1 trusted 输入通道：chrome.debugger 为主，系统级注入为备选（不默认做）
- chrome.debugger：CPS Input 事件 isTrusted=true（Puppeteer 全部走此路）；横幅无法避免（仅企业策略部署可免，Chromium 源码佐证）；attach 期间 SW keepalive 由框架保证，不需要保活 hack。
- 系统级注入（Native Messaging + SendInput/CGEventPost）：唯一"事件层面完全等同真人"的路线（无横幅无 CDP 痕迹），但要分发本地二进制 + macOS 辅助功能授权——**列为二期可选，等 DOM 通道被风控封死再上**。
- 排除项（调研确认不可用）：chrome.input.ime（仅 ChromeOS）、VirtualKeyboard API（只管显隐）。
- **中文输入拟真**：CDP `Input.insertText` 整段注入与真人 IME composition 序列不同，是可检测特征；`Input.imeSetComposition`（实验性）可产生完整 composition 序列——列为增强项，Playwright 官方也尚未支持（issue #5777）。

### 3.2 发评论成功判定：三段式协议（Postiz 模式）
```
post_comment() → {status: pending, pendingData: {commentText, noteId, ts}}
  ↓ Go 后端轮询（只读、幂等、重试无害）
verify_comment(pendingData) → DOM 轮询评论区是否渲染目标文本
  ↓ 出现 → finalize（落库 commentId/时间戳）
  ↓ 4s 未出现 → 判定 blocked/rejected → markBad + 归因
```
契约默认失败：适配器声明 pending 语义就必须实现 verify，否则首次测试即报错（fails-loudly，防静默假成功——R17 的教训）。

### 3.3 事件序列规范（写进基座常量，禁止再手拼）
逐字符：`keyDown{text=char, unmodifiedText=char, key=char, code=<物理键>, windowsVirtualKeyCode=<正确键码>}` + `keyUp`；Enter 用 `text:'\r'`；鼠标 `mouseMoved(button:"none")` → `mousePressed{buttons:1, clickCount:1}` → `mouseReleased{buttons:0, clickCount:1}`。参数表以 Puppeteer cdp/Input.ts + USKeyboardLayout.ts 为唯一事实来源。

### 3.4 账号池
v1 不做多账号池（单用户单账号，per-user Host 已天然隔离指纹+登录态）。Account 实体先落模型与三态机，池调度留接口。依据：闲鱼 ai-goofish-monitor（14.3k★）"扩展导出登录态"方案与我们的架构天然契合；多账号指纹池（AdsPower 类）原理文档未公开（调研明确标注未找到），开源替代是 stealth+fingerprint-suite，属二期。

## 4. 分期路线（每期可独立验收）

| 期 | 内容 | 验收标准 |
|----|------|---------|
| M1 基座重构 | L3 适配层接口+注册表；小红书适配器四件套（现散落的 xsec_token/content-textarea/button.submit 知识迁入）；post_comment 补 mouseMoved 前置+clickCount 修正；新增 assert/query/wait_for 原语；事件日志表 | 现有小红书发评论功能回归通过；新增平台=纯新增目录 |
| M2 闲鱼 | 读链路（商品详情+搜索，mtop 签名 MD5）+ 登录态导出；评论区发帖**依赖真机抓包确认接口**（调研明确标注未找到公开资料） | 能读商品+搜索；发评论结论以真机为准 |
| M3 抖音 | 读链路（a_bogus+msToken）；发评论优先签名 API（comment/publish 公开）；DOM 路线无公开选择器，真机验证 | 读通；发评论结论以真机为准 |
| M4 增强 | humanize 层（节奏/轨迹随机化）；imeSetComposition 中文拟真；账号池；DBOS 断点续跑（若并发上量） | 风控特征降低的量化对比 |

## 5. 风险与未决问题（实事求是）

1. **小红书评论被静默吞**：R17 实测技术链路全通但评论区无新评论。xiaohongshu-mcp 佐证"账号被限制"是已知现象——**输入方式不是唯一因素**，账号权重/内容频率是同权重变量。基座能做的是把「提交→验证→归因」闭环做实，让每次失败可归因。
2. **闲鱼评论发布接口无公开资料**：必须真机抓包（登录态浏览器 F12）才能定，M2 前置任务。
3. **抖音 DOM 输入框无公开选择器**：社区全走 API 路线；M3 若走 API 需处理 a_bogus 版本迭代（2024-09 大版本变更先例，社区需持续跟进）。
4. **chrome.debugger 横幅**：无法避免，用户心理成本；企业策略部署是唯一合规免横幅路线（不适合个人部署场景）。
5. **CDP Runtime.enable 检测**：原理社区公认但一手来源 404（调研明确标注）；MV3 扩展不走外部 CDP 客户端，天然规避，此优势写进基座原则。
6. **行为节奏量化阈值**：无权威研究（调研未找到），humanize 参数靠实测调优。
7. **合规**：各平台 ToS 禁止自动化；批量自动评论属灰色/违规。基座定位为个人助理工具（单账号、低频、用户自己的账号），不做批量矩阵设计。

## 6. 来源清单（核心）

- MDN Event.isTrusted；Puppeteer cdp/Input.ts + USKeyboardLayout.ts + issue#1804/#9889；Playwright docs/input + PR#39493 + issue#5777（IME 局限官方承认）；Chromium debugger_api.cc（横幅 suppress 条件）
- xpzouying/xiaohongshu-mcp（15.7k★）：comment_feed.go 选择器 + waitCommentRendered 就地校验 + humanize 包——发评论 DOM 路线的标杆实现
- NanmiCoder/MediaCrawler（64.7k★）：三平台签名头/端点/461 验证码逻辑，"浏览器上下文取签名"路线原话
- Cloxl/xhshow（1.1k★）：小红书 X-S 纯算 + x-rap-param 写操作风控头 + SessionManager
- Evil0ctal/Douyin_TikTok_Download_API（20k★）/ f2：抖音 comment/publish 端点与 a_bogus
- cv-cat/XianYuApis（1.3k★）：闲鱼 WS 私信协议 + MD5 签名；shaxiu/XianyuAutoAgent（9.1k★）
- Postiz（social.abstract.ts 三段式验证协议）/ Mixpost（SocialProvider 接口）/ LaVague（四层架构+扩展驱动）/ Playwright actionability / Midscene（三分类 API+缓存禁用）/ Skyvern（Run 产物模型）/ Apify SessionPool（retire/markBad/markGood）/ Temporal+DBOS（事件日志重放）
- 本机实测（2026-09-09）：三平台无头拦截形态复现、小红书 SSR 游客可读+评论 CSR、闲鱼 RGV587 实弹响应

> 未找到/存疑项均已在调研报告中显式标注，未编造。

---

## 7. 二轮补调研增补（CDP 实现级，源码已逐行核对）

### 7.1 键入通道的关键修正（对我们 R17 实现的纠错）
1. **CJK 一律不走 dispatchKeyEvent**：`Input.insertText` 是唯一的中文通道（Chromium input_handler.cc：InsertText → ImeCommitText，等价一次 IME 上屏，产生 trusted 的 beforeinput/input 事件；USKeyboardLayout 无中文条目，VK=0 按键语义不可靠）。Puppeteer type() 的 fallback 即如此：ASCII 可映射字符走 keyDown/keyUp，其余逐字 insertText。
2. **逐字 insertText + 间隔是拟人正解**：整段插入=1 次 commit=1 组 input 事件；逐字=N 次 commit，React onChange 每字触发（xiaohongshu-skills 逐字 50ms、xhs-mcp humanize Keystroke 中位 120ms 范围 30-400ms）。
3. **鼠标序列补全**（我们 R17 缺失项）：`mouseMoved{button:"none",buttons:0}` 前置必须 → `mousePressed{button:"left",buttons:1,clickCount:1}` → hold 45-250ms（中位 84ms）→ `mouseReleased{buttons:0,clickCount:1}`。down/up 的 clickCount 必须一致。
4. **infobar 坐标陷阱**（A9T9 源码实测）：debugger attach 的黄色横幅使 innerHeight 收缩 ~56px，动画期间坐标采样错位 → 对策=等 innerHeight 连续 3 次采样不变（上限 1500ms）或 attach 后固定 sleep 500ms 再取坐标。
5. **SW 生命周期利好**：Chrome 118+ 起，活跃 debugger session 保持扩展 SW 存活（官方 lifecycle 文档原文）——长流程无需保活 hack。
6. **executeScript Promise 语义**：注入函数返回 Promise 时浏览器等待 settle——「注入 MutationObserver 等 `textContent.includes(评论文本)` 出现再 resolve」可一次 executeScript 完成就地验证（配超时兜底）。
7. **可照抄的开源参考实现**：autoclaw-cc/xiaohongshu-skills（同为小红书场景：mouseMoved 5 步轨迹每步 8ms + press sleep(30ms) + release；contenteditable 用页面内逐字 execCommand('insertText')）；A9T9/RPA cdp_input（事件序列工厂 + onDetach 单例 + 3s idle 复用 attach + innerHeight 稳定检测）；midscene chrome-extension（already-attached 容忍 + detach 类错误重 attach 重试 2 次）。
8. **attach 后副作用**：alert/confirm 被 debugger 客户端接管（Chromium web_contents_impl.cc 源码核实，官方文档未写）；`Another debugger is already attached` 可容忍；`Debugger is not attached` 类错误自动重 attach 重试 2 次。

### 7.2 对 R17 实现的修正清单（M1 落地项）
| # | R17 现状 | 修正 |
|---|---------|------|
| 1 | cdpTypeText 直接逐字 dispatchKeyEvent | CJK 逐字改 `Input.insertText`；ASCII 才走 keyDown/keyUp（码表按 §7.1.1） |
| 2 | 缺 mouseMoved 前置 | 补 `mouseMoved(button:"none",buttons:0)` → settle 200-1200ms → press → hold → release |
| 3 | press/release 无 clickCount | 双方补 clickCount:1；buttons 位图正确（按下 1/松开 0） |
| 4 | attach 后立即取坐标 | 补 innerHeight 稳定等待或固定 sleep 500ms（infobar 高度陷阱） |
| 5 | 验证只看输入框清空 | 补 MutationObserver 就地验证评论区渲染出评论文本（超时兜底），pending→verify 三段式 |
| 6 | detach 后无重试 | 补 onDetach 监听 + detach 类错误重 attach 重试 2 次 + 防御性 detach |
| 7 | 事件序列/码表散写 | 抽扩展内 `cdp/input.js` 模块：USKeyboardLayout 子集 + 事件序列工厂（照抄 A9T9 模式） |
| 8 | 每次操作重复 attach/detach | idle 复用 attach（3s 延迟 detach），减少横幅闪烁次数 |

### 7.3 已核实未找到（不编造）
- Input.imeSetComposition 的生产使用者清单——未系统核实（协议/Chromium 实现已确认，experimental）
- attach 后 alert/confirm 被接管为源码级核实，官方文档未写；「不应答是否永久阻塞 renderer」未逐行核实
- 适配层/事件日志的专项调研 agent 失败——采用首轮调研报告的 Postiz/Mixpost/Apify/Temporal 结论（已够用），接口细节按首轮报告的 Go 翻译建议落地
