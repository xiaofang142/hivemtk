# ChatGPT-On-CS ↔ hivemtk 三方审计（别人怎么做 / 我们怎么做 / 做到什么程度）

> **性质**：从头到尾二次调研论证（2026-11-06）。①对方做法全部基于 v1.4.5 官方安装包还原源码
> （证据链见 [CHATGPT-ON-CS-REAL-DRIVER-2026-11.md](./CHATGPT-ON-CS-REAL-DRIVER-2026-11.md)），
> 本轮补读了此前未读的拼多多/千帆/知乎/小红书专业号/抖音企业号/桌面类与调度内核；
> ②我方做法基于 hivemtk `master`（78d403ab）源码与在库测试；③完成度矩阵的每个格子都有
> 可复核证据（代码路径 / 测试名 / 本轮复跑读数）。
> **证据标记**：`实证`=还原源码可直接指出；`测试`=在库测试名；`本轮复跑`=2026-11-06 执行记录。

---

# A. 别人怎么做（ChatGPT-On-CS v1.4.5，全量精读结论）

## A1. 总体形态

单机桌面应用（Electron + Python 侧车 + SQLite），单运营者、无租户、无服务端。
统一接待 = **一个 2 秒心跳串行轮询 + 14 个平台策略类 + 三种驱动形态**。

## A2. 调度内核（strategyService.ts，709 行，实证）

```
setup(): while(true):
    for strategy in strategies:            # 全平台串行，无并发
        strategy.action()                  # 按状态机分派
        strategy.saveStorageState()        # 每轮落 cookies/localStorage
    sleep(2s)                              # 全局唯一节拍
页面/浏览器关闭错误 → reset(): 全部 stop + browser.close() + 清空注册表
```

- 状态机：`START`（goto URL）→ `INIT`（等登录/等页面，最多 3 次尝试后自杀）→
  `RUN`（stepRun 收发）→ `DESTROY`。
- `updateTasks(instances)`：DB `instance` 表 ↔ 内存策略列表**对账**（Mutex 串行）：
  删除消失的、暂停 `is_paused` 的、新增新的。UI 的"添加任务"就是往这张表插一行。
- 每个 (平台, 实例) = 一个策略对象；BrowserContext 按"同平台不共用"复用
  （`getContextForType`：找一个不含该 type 的 context，否则新建）。
- VIP 门控：`getLazyConfig()` + `membership.vipLevel/expirationDate` 决定平台 `impl`；
  win_wechat / bilibili / zhihu / weibo 恒可用（引流款）。

## A3. 三种驱动形态（实证）

| 形态 | 载体 | 平台 | 关键实现 |
|---|---|---|---|
| ① 网页 Playwright | Node 主进程，`chromium.launch(用户本机 chrome.exe, headless:false, --disable-blink-features=AutomationControlled)` | douyin、douyin_mp、jinritemai、pinduoduo、weibo、xiaohongshu、xiaohongshu_pro、zhihu、bilibili | 每平台一个类：`url/waitUrls + init(等登录) + stepRun(枚举会话→解析→getReply→发送)`；解析用 **JSDOM 解析 innerHTML 快照**；长回复按 `truncateWordKey/Count`（默认 210 字）分段；图片回复走 `uploadFile(input[type=file])` |
| ② 桌面侧车（socket.io） | Python 侧车 `__main__.exe`（Nuitka）：win32gui 窗口句柄 + Airtest 模板匹配（cv2）+ pyocr（onnxruntime） | win_wechat、win_qianniu、wecom（注释停用）、win_jinmai | 主进程只转发 `strategyService-run {app_id,cfg}`；侧车扫桌面窗口 → 截图 OCR → emit `messageService-getMessages` 要回复 → 键入发送。函数名实证：`wechat_get_window_handle/get_editor_rect/send_to_editor`、`qianniu_get_message_box_handle`、`WeWorkWindow` |
| ③ 桌面 CDP | Node 内 `spawn(千帆客户端 --remote-debugging-port=N)` + `chrome-remote-interface` | win_xiaohongshu_eva | 先探端口再启动；冲突时 `taskkill` 强杀重拉；CDP 直连页面协议 |

登录方式统一为**用户在可见浏览器窗口里人工扫码**（`waitUrls` 等跳登录页，实例间 cookie
持久化在 `~/Documents/chatgpt-on-cs/sessions/session_<id>/`，每轮保存）。

## A4. 十四平台驱动速查（全部实证）

| 平台 | 入口 | 会话/未读 | 方向判定 | 输入/发送 | 上下文抓取 |
|---|---|---|---|---|---|
| douyin | creator.douyin.com/…/chat | `div.semi-tabs-tab`、用户列表 | — | `div[class*='chat-input']` fill+Enter | — |
| douyin_mp | e.douyin.com/site/douyin-mp/…/imchat/workspace | 同上族 | — | 同上 | — |
| jinritemai(抖店飞鸽) | im.jinritemai.com/pc_seller_v2/main/workspace | `#rc-tabs-0-tab-current`、通知列表点父级 | — | `.messageList` 页 `fill`+Enter；图 `qa-send-img` | **商品卡** goodsAvatar/goodsName；转人工 `qa-transfer-customer`；掉登录自动 goto fxg.jinritemai.com/login |
| pinduoduo | 拼多多商家工作台 | `.chat-portrait > i` 未读 | — | `#replyTextarea` + `.reply-footer .send-btn`（Enter 兜底）；图 `input[type=file]`+`.btn-ok` | **最强**：右栏点"最新订单"→"个人订单"→"全部"→读 `.order-item`（状态/单号/商品/金额/规格→CTX_*）；"商品推荐→浏览足迹"抓当前商品；**备注随机化防风控** |
| weibo | api.weibo.com/chat#/chat | `[class*='num fff font']` | 气泡 `bubble_cont basi` **left=对方/right=自己** | `#webchat-textarea` fill+**Enter**；图 `i.icon.icon-msg.tp`+file input | — |
| xiaohongshu | xiaohongshu.com/notification（**评论/通知运营向**） | tabs 切换 | — | `.action-text` 开评论框 → `.comment-input` → `button.submit`；含关注列表回访、关弹窗 `.close-circle` | 笔记/作者名 |
| xiaohongshu_pro | pro.xiaohongshu.com/enterprise/message/reply | `.contact-meta` | — | `[class*='message-list window-body']`、`[class='input-area']` | 绑定操作 `bind-action` |
| zhihu | zhihu.com/messages | `.ChatListGroup-Section:first-child` 未读 | — | `.InputBox-input>.Input` | — |
| bilibili | message.bilibili.com/#/whisper | `.list-container .list-item` + `.notify.notify-number`；未关注人 tab | `.msg-item` class `not-me` / `.message-content` class `is-me` | `#editor` fill + `[class*='btn-box send-btn']`（Enter 兜底）；图 `.image-upload-btn` | — |
| win_wechat | 微信 PC 客户端 | 侧车窗口扫描 | OCR+坐标 | 剪贴板→`wechat_send_to_editor`+Enter（快捷键必须 Enter） | 截图 OCR |
| win_qianniu | 千牛客户端 | `qianniu_get_message_box_handle` | — | `qianniu_send_to_editor`；发图走 HTTP | — |
| wecom | 企微客户端 | `WeWorkWindow` | — | 同族 | 41 张模板（新好友/群管理等） |
| win_jinmai | 京麦客户端（=京东商家） | `airtest/jinmai/chat_message_input.png` | — | 同族 | — |
| win_xiaohongshu_eva | 千帆客户端 | CDP 直连 | CDP DOM | CDP | — |

## A5. 回复链（Node 内，全部平台共用）

转人工关键词命中 → 直接 `TRANSFER`；→ 按 `context_count`（默认 20）截最近消息；
→ 拟人延时（`reply_speed ~ +reply_random_speed` 秒）；→ 关键词匹配（`|`/`[and]`/正则/模糊，
回复支持 `[or]` 随机、`[~]` 随机符、`[@]文件[/@]`）；→ LLM（9 家供应商，流式拼接）；
→ 替换词（微信→V兴 等平台违禁词规避，内置种子）；→ 默认回复兜底。
自定义插件可替换整条链（vm 沙箱，预置 fs/path/axios/os/url/crypto + config_srv/reply_srv）。
手机号/商品提取随回复写本地 txt（`extractMsgInfo`）。

## A6. 对方的结构性局限（我们的机会面）

1. **无幂等**：消息无稳定 ID 唯一键，靠"最后一条消息 class"判定方向，innerHTML 快照重扫
   ＋ 每轮全量 action——重发/漏发靠 `NO_REPLY` 与人工，无 at-least-once 保障。
2. **无投递状态机**：回复发出即结束，发送失败仅日志；无 ack/重推/死信。
3. **单机单用户**：SQLite + 本地 Chrome + 2s 串行节拍——平台数 × 实例数一多，轮询周期即膨胀；
   无水平扩展。
4. **无多租户/权限/审计**。
5. **桌面驱动闭源且 Windows-only**（win32/Airtest/tasklist）。
6. 但它有而我们弱的：**电商上下文主动抓取**（订单/商品/物流进 CTX_*）、**转人工关键词**、
   **替换词**、**关键词回复运营三件套的成熟 UI**、**分段发送**（truncateWord）、
   **拟人鼠标轨迹**（humancursor）、**OCR 读屏**。

---

# B. 我们怎么做（hivemtk 78d403ab，对账）

## B1. 总体形态

服务端多租户 SaaS（Go 五层 + PostgreSQL/Redis + 推理栈），渠道以**插件化适配器**接入；
浏览器/桌面侧只做"采集与代发"，决策、幂等、账本、风控全在服务端。

## B2. 渠道接入家族（真实代码路径）

| 家族 | 渠道 | 入站 | 出站 |
|---|---|---|---|
| 蜂桥扩展（MV3） | douyin、xiaohongshu、kuaishou(实验)、xianyu、tiktok、**jd/bilibili/weibo(实验)**、personal_wx(协议就绪) | content script → `POST /api/bridge/ingest`（HTTP 三通道 + SSE 下行 + ack；另有 `/api/ws/channel` WS 传输） | outbox 拉取 → 扩展回写 |
| channelbot | telegram、whatsapp、qq | webhook / 轮询 | Bot/Cloud API |
| webhook 官方 | 公众号(wechat/weixin)、wecom、feishu、dingtalk | 验签回调 | 官方 API |
| web/web_embed | 网页客服组件 | 长轮询/WS | 站内 |
| 触达 | sms、email、card | — | 阿里云/SMTP/卡片短链 |
| 运营自动化 | browser_automation（douyin/xiaohongshu/xianyu 评论） | — | Playwright 基座 + 平台适配器四件套 |

渠道词表单源：`model` 常量 → `channelgw.Default` 注册表（含 Experimental 声明）→
`message_hub` 词表 → `bridge` 别名归一 → `capregistry` 能力矩阵，五处由测试锁死
（`registry_test`、`TestMessageHubPlatformsCoverAllChannelConstants`、`channel_new_test` 等）。

## B3. 入站链（`inbox_ingress.go:366` HandleIngressMessage，实证）

```
NormalizeEvent（补 event_id/session_id/会话键）
→ ① msg_id 幂等：hub 已有同 id 同会话 → 跳过；跨会话命中 → 各自入库（内容指纹允许复用）
→ ② 人工锁判定（坐席接管中 → 只落库不进 AI）
→ ③ interceptInbound 中间件：内容窗口去重 + 出站回声拦截（自声跳过、真话保留）
→ ④ persistMessage 落 message_hub（失败要退还去重键——先占后写的坑位管理）
→ ⑤ 触发 AI（批处理合并 + 锁 + 预算限制，见 InboxRecheckBudget*）
```

## B4. 出站链（outbox 状态机，实证 + 测试）

```
AI 回复 / 人工代发 / 卡片
→ DeliverBridgeOutbound（bridgeChannels 白名单 + Markdown 剥离 + 不可达预检）
→ message_hub direction=outbound status=pending（内容哈希 msg_id）
→ 扩展 GET /api/bridge/outbox：ClaimPendingOutbound——
   pending→认领（claimed_at 租约，InboxOutboundClaimTimeout 内不重发；
   超时 stale reset 回 pending；并发认领不重复：TestInboxIngress_ClaimPendingOutbound_ConcurrentNoDuplicate）
→ 扩展回写网页 → POST ack（v2 items[]）→ delivered / failed（幂等，越权防护）
→ 不可达预检失败直接 failed+原因（不留假 pending）
```

## B5. 与对方逐项对比（谁学谁、谁强谁弱）

| 维度 | 对方（v1.4.5） | 我们 | 结论 |
|---|---|---|---|
| 消息幂等 | 无（快照重扫） | msg_id + 内容指纹 + 回声拦截 | **我们强**（服务端账本必需） |
| 投递可靠性 | 发完即完 | outbox 租约/ack/stale/死信 | **我们强** |
| 多租户 | 无 | tenant 贯穿 + 隔离闸 | **我们强** |
| 采集方式 | innerHTML 快照 + JSDOM（简单但重扫） | MutationObserver 增量 + 会话锚点防错归 | 各有取舍；我们更实时，对方更抗 SSR 抖动 |
| 渠道覆盖 | 14 平台（4 桌面） | 蜂桥 8 网页 + 官方 API 家族 + 桌面协议就绪 | 对方多**桌面**与拼多多/知乎/抖店；我们多**官方 API**与多租户 |
| 电商上下文 | CTX_* 主动切 tab 抓订单/商品/物流 | extra 有通道、无抓取实现 | **对方强**——已登记借鉴 |
| 转人工/关键词/替换词 | 成熟（含运营 UI） | 有人工接管/坐席；无关键词/替换词 | **对方强**——登记 |
| 分段发送 | truncateWord 210 字 | 服务端 4KB 截断 + 扩展拟人键入 | 基本对齐；细分策略未搬 |
| 拟人化 | 随机延时 + humancursor 轨迹 | 拟人键入/令牌桶/会话冷却/内容去重 | 接近；鼠标轨迹未搬 |
| OCR 读屏 | onnxruntime 侧车 | 无（桌面伴生未建） | 对方强，随伴生工程 |
| 插件 | vm 沙箱（同进程，无超时） | modulekit 体系 | 我们更安全；对方更好用（试跑 UI） |

---

# C. 做到什么程度（完成度矩阵 + 本轮复跑读数）

## C1. 六渠道统一接待完成度矩阵

图例：✅=代码+测试在库；🧪=本机全链路（真实服务端+DB）绿；🔴=外部腿未做（真机/桌面伴生）

| 渠道 | 注册/词表 | 扩展/驱动 | 入站🧪 | 出站🧪 | 前端标签 | 单测 | 文档 | 真机 |
|---|---|---|---|---|---|---|---|---|
| 微信公众号 | ✅（既有） | 官方 webhook | ✅ 既有 | ✅ 既有 | ✅ wechat→微信公众号归一 | ✅ 既有 | ✅ | 生产可用（历史） |
| 企业微信 | ✅（既有） | 官方 webhook | ✅ 既有 | ✅ 既有 | ✅ | ✅ 既有 | ✅ | 生产可用（历史） |
| 个人微信 | ✅ bridge 注册(实验) | 🔴 桌面伴生未建 | 🧪 | 🧪 | ✅ | ✅ | ✅ | 🔴 |
| 小红书 | ✅（既有） | ✅ 蜂桥 | 🧪 | 🧪 | ✅ | ✅ | ✅+实证对照 | 历史真机 |
| 抖音 | ✅（既有） | ✅ 蜂桥 | 🧪 | 🧪 | ✅ | ✅ | ✅+实证对照 | 历史真机 |
| 京东 | ✅ bridge 注册(实验) | ⚠️ 网页桥零实证（京麦桌面=路线B 未建） | 🧪 | 🧪 | ✅ | ✅ fixture | ✅ 路线修订 | 🔴 |
| B站 | ✅ bridge 注册(实验) | ✅ 蜂桥（实证选择器） | 🧪 | 🧪 | ✅ | ✅ 实证 fixture | ✅ 实证版 | 🔴 |
| 微博 | ✅ bridge 注册(实验) | ✅ 蜂桥（实证选择器） | 🧪 | 🧪 | ✅ | ✅ 实证 fixture | ✅ 实证版 | 🔴 |

## C2. 本轮复跑读数（2026-11-06，审计轮最终值）

| 项 | 命令 | 读数 |
|---|---|---|
| Go 编译/门禁 | 干净 worktree（HEAD=78d403ab）`make vet` + `go build ./...` + `make lint` | **vet 通过 / build OK / golangci-lint 0 issues**（工作区未暂存的并行 geo 半成品致主工作区红，与本任务无关，取证用 HEAD 检出） |
| 扩展测试 | `cd user-web/bridge && npm run test` | **78 文件 929 通过 / 7 skipped** |
| 扩展构建 | `npm run build` | 8 入口产物 OK |
| 前端渠道标签 | `npx vitest run channel_options_new_channels` | 5 通过 |
| 文档链接 | `check-md-links-offline.py`（暂存后） | 0 断链 |
| curl 六渠道 | HEAD 构建服务端 + 真实 PG/Redis：ingest→hub→outbox→ack | **douyin/xiaohongshu/jd/bilibili/weibo/personal_wx 全 delivered（PASS=6 FAIL=0）** |
| ego browser | 三渠道 fixture 解析/发送 + popup | 解析：jd `jd-1001`、bilibili `conv:测试用户`、weibo `20001`（first=customer/second=self 方向实证生效）；发送：JD 点击/B站点击/微博 Enter 均写入并清空；popup 7 入口 8 tab |

## C3. 审计结论与遗留

1. **借鉴到位**：渠道注册表单源+实验降级、扩展三通道协议、outbox 状态机、多租户与幂等——
   这套"服务端化"的重写方向被对方架构反证是对的（对方缺的恰是我们有的）。
2. **尚未搬**（登记为后续，非本轮债）：电商上下文 CTX_* 抓取（拼多多订单面板模式）、
   转人工/关键词/替换词三件套、truncateWord 分段、humancursor 鼠标轨迹、桌面伴生（微信/京麦，
   与对方 win32+Airtest+OCR 同路线）。
3. **外部腿不变**：B站/微博/京东网页真机登录回归；个人微信与京麦桌面伴生工程；抖店飞鸽入口。
4. 本轮未发现需要当轮修复的缺陷：测试、构建、回归、文档链接全绿。
