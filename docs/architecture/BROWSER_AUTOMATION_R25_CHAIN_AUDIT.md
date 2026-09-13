# 浏览器自动化 — 全链路功能清单与逐项清醒论证（R25 第一步）

> 2026-09-11 ｜ 依据主文档 v1.2；R24 改动后全链路回归视角。
> 方法：每一环 = 代码锚点复验 → 清醒三问（契约兑现吗/失败会怎样/声明与事实一致吗）→ 结论 ✅/🟡/❌ + 修正项。

## 链路清单（S0–S9，含每环的契约与失败形态）

| # | 环节 | 载体 | 契约（做什么/不做什么） | 失败形态与归宿 | 论证结论 |
|---|------|------|------------------------|--------------|---------|
| S0 | 前端控制台 | user-web Vue3：List/Editor/Detail/Monitor(含命令流面板)/Cron(含时区)/Status + api 28 端点 | 只发 REST，不碰浏览器 | 409 引导装扩展；500 提示 | 🟡 dist 已重建但**浏览器未重载新 dist**（旧 1.2.0 页面缓存） |
| S1 | Gin 路由 | browser_automation_routes.go：JWT 26 端点+WS 独立+DI 装配 | Router 零逻辑 | 启动期 panic 即拒 | ✅ R24 验证（logs 端点在线实测） |
| S2 | Executor 双模式 | executor.go：steps 解释 / Brain 循环；步级 retry+backoff；humanizedDelay；DetectBlock 步后；F2① 写禁重试；F7 错误分线；F6a 轮内截断；G17 screenshot 硬拒 | 状态收口只由它写；不持平台知识 | 失败步→abort→session failed+llm_summary 归因 | ❌→已修：F1 trusted 路径的 href 兜底**双跳缺陷**（详 §2-Q1） |
| S3 | Hand+Registry | hand.go 17 方法（超时表）；host_registry.go：per-user 路由+req_id pending+**心跳 30s/90s**；顶号+断连清理钩子 | 单 Host 串行；不启动子进程 | 离线→ErrHostOffline→409；超时→error 帧 | ✅ 代码过（race 测试绿）；**运行态当前离线**（count=0，S5 断） |
| S4 | nm-host | cmd/nm-host/main.go：4B native-order 帧（≤1MiB/4GB）+双泵+WS 退避 2s→60s；hostVersion 1.3.0 | Chrome 的子进程；无 token 等 30s | chrome_write 错误帧快速失败 | 🟡 二进制 /usr/local/bin/ 已装；**进程未活**（扩展未 connectNative 拉起） |
| S5 | MV3 扩展 SW | browser_automation/：connectNative 重连 5 次+dispatch 16 case+@eN refs+**F1 trusted 主通道**+F4 actionability+F3 贝塞尔+三段式 post_comment | 序列化注入铁律；SW 内存 refs 导航失效 | disconnected→popup 面板可见 | ❌ **运行时断链点**：需 chrome://extensions 重载 dist（v1.3.0 新代码生效）+ 确认登录三平台 |
| S6 | L3 平台 | platform.go 注册表；三适配器 Locators/DetectBlock/ClassifyError；CommentPoster 仅小红书；**能力矩阵诚实** | fails-loudly；init 自注册 | 未声明→基座拒 | 🟡 诚实性有单测锁；**douyin/xianyu 写能力待真机 DOM 探查**（第三步） |
| S7 | Brain | brain.go reflect+judge；brain_prompts 模板工厂；**F5 独立快照复核+T1 分隔**；预算/钳位/分类重试 | 平台知识经注册表注入；禁 screenshot（轮内） | 连败/预算/看门狗→failed | ✅ R24 落地；judge 走真机回归待跑 |
| S8 | 数据面 | 六表+迁移 v3.37–v3.40；command_log append-only+**D1 读侧**；llm_plans 成本账；**D4b 重试持久化/G19 裁剪** | 状态机收口 | DB 错→告警不阻断 | ✅ psql 实核三列已建 |
| S9 | 触发入口 | 手动/cron（**G20 时区**）/MCP/workflow + **F8 并发闸**（ErrUserBusy） | 闸=单用户串行 | cron 命中闸=本轮跳过 | ✅ 真机实测 409（DUPLICATE_ENTRY_3003） |

## §2 清醒论证发现的问题（本轮修正）

**Q1（真机前必改，已修）：F1 trusted click 的 href 兜底会双跳。**
R24 实现里 click 走 trusted 后仍执行 injHrefFallback 接管 `_blank`/跨 origin 链接——但 CDP trusted 点击是真实导航：`_blank` 链接已由浏览器开新标签，再 `location.href=` 会把**当前自动化 tab 也跳走**（用户看的是新标签，session 却丢了原页面）。DOM 兜底路径保留接管是历史正确行为（合成事件常不导航）；trusted 路径不需要也不该有。
**修正**：probe 返回点击前 `href`，trusted 点击后复读 `location.href`，`navigated=hrefChanged`——F6a 证据也更真实。

**Q2（运行态，非代码缺陷）：链路当前断在 S5。** 重启 user-server 后旧 WS 连接已死、扩展未重连、nm-host 未运行。S3 心跳保证今后僵尸连 90s 内判死。**恢复动作=chrome://extensions 重载 dist**（顺带完成 v1.3.0 升级）——列入第三步 0 号前置。

**Q3（能力诚实性预判）：** 第三步要求三平台都"评论"。当前能力矩阵只有小红书声明 post_comment。抖音/闲鱼评论框**公开资料为零**（本轮网络调研再次确认：f2 评论仅"未来实现"、闲鱼详情页留言无公开结构信息）——解法不是猜选择器，而是**用我们自己的 snapshot/query 在已登录真机上探查真实 DOM**（用户本人 Chrome=最高权限侦查工具），把实测选择器写进适配器、按实调整能力声明。

**Q4（已核无问题项）**：stepChangedPage 解析 click 回包 navigated（Q1 修正后语义不变）；detectBlockedIfFatal 每步后重拍快照与 F6a 不冲突；post_comment 坐标点击受益于 F3 抖动（按钮>16px 安全）；F8 闸与 cron RestoreAll 兼容（闸在 RunTask 内，cron 命中=跳过本轮）。

## §3 与 R24 状态衔接
R24 已实施项（见主文档 v1.2 修订记录）不在本文重复；本文是**真机执行前最后一道纸面闸门**。清单执行结果回写本文档 §4（已填）与主文档。

## §4 真机执行结果（R25 实施轮，2026-09-13）

### 执行面（v1.4.0 扩展 + user-server.r25c，全部真机跑通）
| 验收 | 载体 | 结果 |
|------|------|------|
| F2② 三段式 | task116/xiaohongshu「今天真好吃」 | ✅ session195：prep→send→finalize 全绿，评论入库验证 verified=true（192 同）；**finalize 在 send 回包超时下仍正确归因成功**（见下 R1） |
| A1 三平台读链路 | task116/119(douyin)/120(xianyu) | ✅ 179 前史+186(douyin 3/3)/187(闲鱼 extract 命中真实商品文本)/195 全 completed |
| A2 Brain 模式 | task121 xhs 取笔记标题 | ✅ session191：21/22 步成，提取「干净饮食合集大大大放送❤️‍🔥」，judge 独立复核+循环 nudge+看门狗全生效；终态收口 P0 见 R2 |
| A3 拟人节奏 | 全任务 | ✅ CDP 贝塞尔+对数正态逐字（cdp-input 单测 6 项）+步间抖动 |
| A4 零人工 | 全任务 | ✅ create→publish→run→终态无人工挂起 |
| A5 可审计 | 191/195 | ✅ command_log 命令/回包/judge 三类帧逐条可还原；finalize 证据进 extracted_data |
| A6 新平台零改动 | R20 已证（douyin/xianyu 仅加目录+注册） | ✅ 维持 |

### 本轮实施清单（R24 挂账四项全闭）
1. **F2② 三段式拆回 Go+finalize**：扩展侧 comment_prep/comment_send/comment_verify 三子命令（post_comment 一站式协议删除，单一路径防分叉）；Go executor prep→send→finalize 轮询（6s+5s+5s，ctx/stopCh 双可中断），证据+posted_at 落 extracted_data（I4 续跑位点）；新 Go 测试 5 组（三段顺序/只读轮询/stopChFor/mergeExtract/公开契约）+扩展测试 6 项。
2. **F6 余项**：① 历史压缩=滑窗 24+溢出折叠台账（[已折叠 N 步: click×21 …] 首行回喂，零 LLM 成本）② 新元素标记=SW 按 tab 基线 diff，新行 `*` 前缀（browser-use *[index] 语义），open_tab/navigated-click 自动清基线。
3. **G9 死代码收口**：删 Brain 三个旧转发壳（唯一入口 GeneratePlanReflect）、Hand.tabExists、BrowserSession.Title/LlmPlan/HandLatencyMs/ConsoleErrors、BrowserTask.LlmPlanID（模型字段删，DB 列按 D6 保留）；UpdateTitleAndSnapshot→UpdateSnapshot、UpdateMetrics 去恒 0 参、UpdateArtifacts 去 consoleErrors 参；BuildLoopNudge 接线（nudge 文案从散落拼接归工厂）；captureVisibleTab 参数笔误修正。
4. **A1–A6 真机回归**：如上表全绿。

### 新发现缺陷与修复（真机才暴露）
- **R1（P0，executor 收口缺陷，真机 session179→181 暴露）：send 超时被当失败即止，不 finalize 归因**。小红书详情页 executeScript/CDP 可被页面主线程堵到超时（send_error=45s 超时**但评论实际已提交**）——结果未知态必须回查而非判死。修：send 任何结局（成功/出错/超时）都走 finalize 验证，verified=true 即步成功（evidence 记 send_error 供审计）；这正是 Postiz 心跳判因矩阵「有心跳后超时=结果未知→回查」语义。**实证=session195：send_error=45s 超时，finalize 见评论渲染，步判成功，任务 completed**。
- **R2（P0，session188 卡 active 7min+）：Brain 超时收敛后终态写回用已取消的 ctx**，DB 写被取消→看门狗白兜。修：ExecuteSession 收口段统一 context.WithoutCancel+120s writeCtx（终态/指标/总结全走它）。实证=session191（修复后）failed 终态正常+llm_summary 落库。
- **R3（证据归属）：finalize 证据取「评论区首条」非「我们的评论」**（session181 拿到无关文本）。两轮收口：先改「含目标文本优先」，实测仍可能误命中引用同文的历史评论→改「全文恰等于目标=own 优先，含目标次之」。session195 verified=true（容器级证据，item 恰等未命中=评论在弹窗折叠区，如实记录）。
- **R4（运行态纪律）：nm-host 假死新形态**——进程活着+host/status 有 1.4.0 但命令帧全超时（session181-190 多轮）；判据=**lightweight wait 任务**（pong 探针）不通即 pkill nm-host 让 SW 重拉，恢复后一次跑通。扩展 v1.4.0 生效判据链=install.sh 版本锚点三处 1.4.0（manifest/src+n.m-host）+ScriptCache 清理+Chrome 冷重启。**→R26-1 已产品化为服务端自愈探针（§5）。**
- **R5（预期待办）：comment_send 45s 内回包在重页面不稳定**——非正确性问题（finalize 兜底归因），但步耗时被拉长；候选优化=executeScript 竞速超时（超时即返明确错误）。**→R26-2 已落地（§5）：注入类 deadline+Go 归因三分。**

## §5 R26 轮（2026-09-13 晚，扩展 v1.4.1）：R25 遗留运行态项产品化

### R26-1 Host 应用面假死自愈探针（host_registry.go）
心跳（D4a）只测传输面；真机暴露「传输活、应用死」=WS/ping/pong 全正常但 stdio→扩展断链、命令有去无回（人工 pkill 才能恢复）。产品化=**命令级超时计数**：Request 超时分支 noteCmdTimeout 累计、回包到达 noteCmdAlive 清零；连续 `consecutiveCmdTimeoutSick=2` 条即服务端主动 `conn.close()`——复用既有 unregister+断连清理钩子（running session 置 failed）+ nm-host WS 退避重连 + SW connectNative 重拉，整链无人工自愈。阈值语义=单条慢命令（截图类 60s）不误杀；close 对 conn=nil 容错（测试探针连接）。
**真机验证（SIGSTOP 注入）**：暂停 nm-host→两条 pong 任务超时→日志「连续 2 条命令超时…判 Host 应用面假死，主动断开触发自愈」+ host/status 清零 → resume/kill 后 SW 重拉注册（1.4.1）→ pong 即 completed。单测 TestCmdTimeoutSickProbe（阈值不清零/清零不误杀/钩子触发/摘除注册表）。

### R26-2 注入竞速 deadline（primitives.js + executor.go）
扩展侧 `raceTimeout(executeInTab, cmd.inject_timeout_ms||15000)` 包 comment_prep/send 的**定位注入**（注入未开始=零副作用可判）；Go 侧归因三分：`*_inject_timeout_*`=点击从未发生→早返「未提交」不烧 finalize；WS 超时=结果未知→finalize 回查（R25-R1 维持）；业务错误=正常失败。扩展测试 2 项（挂起 Promise 精确错误名）+Go TestInjectTimeoutAttribution。
版本链三处 1.4.1。vitest 42 全绿、Go internal 全包全绿、真机回归：pong 196-201（含假死注入两轮）+ post_comment 快乐路径 202 completed（send 45s 超时态 finalize 正确归因成功，评论已入库）。

### 仍开放（挂下轮）
- send 的 45s 超时本身未消除（CDP 事件序列在重页仍慢）——现归因正确、耗时可容忍；根治候选=cdpInput.clickAt 内部事件批量化/降低单条 round-trip（动 trusted 主通道时序需谨慎，单独立轮论证）。
- 探针阈值 2 为保守值；若真机出现「连续 2 条合法慢命令」误杀证据，再议按 action 分级阈值。

### 遗留挂账
- Brain A2 目标「h1 提取」在 xhs 弹窗 DOM 无 h1 时靠 judge 拒绝+自恢复滚动换路（21 步成），效率待观察不判缺陷。
- SMTP 本地未配（失败通知邮件 warn 属预期噪音）。
- 抖音/闲鱼 post_comment 能力仍不声明（诚实矩阵维持）。
