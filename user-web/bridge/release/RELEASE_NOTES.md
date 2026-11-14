# HiveBridge 蜂桥 v1.3.4 发布包

- 产物：`release/hivebridge-1.3.4.zip`（解压后含 manifest.json，可直接「加载已解压的扩展程序」）
- 构建时间：2026-11-14T10:32:54.676Z

## 本版变更（v1.3.4）
- 【对抗审查回退】hidden 页跳过认领/处理（1.3.3 引入）已回退——本扩展常态运行
  形态就是后台/不可见页（ego 隔离窗口恒 hidden），6 小时 14 条 delivered 全部发生在
  hidden 页；hidden 跳过 = 全渠道下发静默死亡（18:05-18:30 生产回归实证，用户截图
  抓到）。回归钉改写：hidden 页必须照常认领与处理。B站 hidden 空转的真因是实验
  渠道会话列表定位失败，20 次重投预算即其收敛机制。

## 历史变更（v1.3.3）
- hidden 页架构收口（2026-11-14 三层分析轮）：后台页签（用户切走/自动化后台页）
  visibilityState=hidden 时会话 DOM 不渲染，openConversation 假失败——B站 9 条在
  hidden 页空转烧重投预算（每次认领 push_attempts+1，20 次即 failed）。
  轮询路 hidden 跳过认领（ack 排水保留）；SSE 路 hidden 跳过处理（行留服务端等
  租约回收重推）；页面切回可见立即补偿一轮（visibilitychange）。省预算+离线语义
  真实化，下发永远发生在页面可操作时。

## 历史变更（v1.3.2）
- 出站互斥队列（2026-11-14 生产实证修复）：批25 的 _outboundInFlight 是计数器，
  sendOutbound 入口不检查就 begin——SSE 推送与轮询双路的两条下发可并发进入临界区，
  A 拟人键入到一半（30 字≈2.7s）B 并发切入另一会话，后半段打进 B 的输入框，
  assertConv 弃用时留半截文字、两段键入交错 ⇒ 下发消息被截断/多段/串台
  （抖音/小红书/微博均有截图实证）。修复：同 tab sendOutbound 严格串行队列，
  每条独占整个临界区（切换+键入+发送+回查）；beginOutbound 重入出声；
  新增 3 用例钉住（并发串行/错误不传染/重入告警）。

## 历史变更（v1.3.1）
- 二次论证修复（2026-11-14 生产数据实证驱动）：
  微博自身标签守卫：PEER_NAME 逗号并集在文档序上先命中自我盒 .user .name，导致 sender_name 全为
  「用户<自身uid>」、720 条落 conv:用户<uid> 自指会话并诱导人工误发；新增 isSelfAccountLabel
  结果守卫（header 命中自我盒时昵称返回空、禁止派生自指会话 id），选择器匹配面不动；
  抖音双写折叠补口：双写以空白连接时总长奇数（如「蜂桥下行手动L 732783」×2），既有对半守卫
  从连接空格起头错位必漏 → 补「去空白对半」折叠，双写不再原样上行打穿服务端自回显精确匹配；
  导出 dedupeRepeatedText 补 7 用例、weibo 守卫补 4 用例。

## 历史变更（v1.3.0）
- 四渠道真机核对补修（2026-11-13 ego-browser 只读核对，复用登录态）：
  B站（P0）消息气泡改用现网 CSS Modules 哈希类 [class*="_Msg_"]/[class*="_Msg__Content"]，
  并新增 outermost 去嵌套（哈希类同时命中 _Msg__Time/__Wrap/__Main/__Content 等 6+ 节点）；
  会话列表同法去嵌套（54 个子元素收敛为 6 个根行，patrol 不再点错目标）；
  会话 id 改读根行 data-id（去 contact_ 前缀）与 location.hash 的 /whisper/mid(\d+)/；
  激活判定改 /active/i（现网类名 _SessionItemIsActive_*，classList.contains('active') 必失）；
  getPeerName 排除「我的消息」等页面级标题（原兜底命中页面标题 ⇒ 常数 conv:我的消息 串台）；
  新增系统消息漏斗（登录操作通知/系统通知/账号在新设备或平台登录成功）；
  match() 增补「消息中心 URL + 会话列表或消息列表」即真（bot 会话无输入框，
  B 站设计如此，原鸡生蛋导致 bot-only 账号适配器永不启动）；getPeerAvatar 增补激活会话行头像。
  小红书（P1）新增系统消息漏斗：24小时内最多只能发1条/暂不支持该消息类型/请到手机端查看
  （真机 .chat-item 里躺着 2 条限流文案 + 1 条不支持卡片，此前以客户消息上行触发 AI）；
  sendText Enter 兜底改 simulateEnterKey（keydown+keypress+keyup）——真机实证输入栏
  唯一按钮无 aria-label、草稿探针形态不变，无发送按钮，Enter 是唯一下行路径。
  微博（P1/P2）match() 增补「chat URL + 会话列表」即真（未开会话无输入框的鸡生蛋）；
  账号改读 .user 盒隐藏 uid（真机 7911766916，此前 $CONFIG 无 uid、全页零 /u/ 链接
  ⇒ 恒返回 weibo-unknown 占位）；会话 id 改读会话项 .name 内隐藏 uid（替代不稳定的
  data-datas 序号与昵称派生 conv: 前缀）；itemName 与 header 反查按盒内真实 uid 剥离尾部 uid 串。
  抖音（P2 升级为账号身份错误）getAccountId 把 window.SSR_RENDER_DATA.app.user.info.secUid
  提为第一候选——真机实证 localStorage 陈旧值与现网 SSR secUid 完全不同，而 /chat 页零
  /user/ 链接，原链路只能落到陈旧缓存 ⇒ 上行账号身份错；会话项 /user/ 链接（指向对端
  用户）降为末位 DOM 兜底。
- 新增回归 test/channel-realdom-20261113.test.js（23 例，fixture 全部按真机 DOM 结构构造）。
- 修复 package.json 与 manifest.json 版本不一致（055fa098 只 bump 了 manifest 到 1.2.1，
  package.json 停在 1.1.0，npm run release 前置门必红）。

## 历史变更（v1.2.1）
- 修复下行 inflight 死锁根因：bridgeFetch headers 双形状兼容 + 删页面直连回退 +
  startBridge 幂等 + setValue tagName 守卫 + bili sendText fail-loud（055fa098）。
- 补记 1.2.0 漏登的变更：bilibili 选择器校准（CSS Modules 哈希类名
  _SessionItem_/_MessageList_/msb-textarea 致 match()=false 无上行，04821491）、
  weibo 选择器校准（chatlist/input/chat-url 漂移致 match()=false 无上行，212afeb7）、
  拼多多商家网页桥 pdd 全链注册（Experimental，2732f9a4）。
- 注：1.2.x 的 manifest 版本为手工 bump，未走 npm run release，故 CHANGELOGS 缺条；
  本版补齐，并在 1.3.0 修复 package/manifest 版本双源不一致。

## 历史变更（v1.1.0）
- 新增三个网页私信渠道：京东（jdm.jd.com / dongdong.jd.com，all_frames）、
  B站（message.bilibili.com）、微博（weibo.com/chat）。三条均为实验渠道：
  选择器来自公开页面结构，未真机校准，popup 与后端均标 Experimental。
- 微博以 Enter 发送（页面原生语义），B站/京东优先点发送按钮、回车兜底。
- 扩展渠道表/展示名/入口 URL/账号启停顺序/选择器面板同步登记；构建产物新增
  content-jd.js / content-bilibili.js / content-weibo.js。
- 渠道差异仍收敛在 BaseAdapter 契约内：会话锚点、串台守卫、拟人键入、
  outbox/ack、限速全部复用，没有为单渠道另造投递链路。

## 历史变更（v1.0.5）
- 修复 1.0.4 的注入失效（严重）：官方发行包即使带 BRIDGE_ONLINE_SERVER 构建，
  弹窗仍预填 localhost、线上按钮被隐藏 —— 产物里是
  `typeof process<"u" && process.env && "https://…" || ""`，而 MV3 扩展没有 process
  全局，该短路在运行时恒假，注入被静默丢弃（真机复现：1.0.4 安装后默认=localhost）。
  改为 esbuild define 直接替换专用标识符 __BRIDGE_ONLINE_SERVER__，注入真正生效。
- 补回归用例 test/popup-online-injection.test.js：带 define 打包 → 断言预填云端、
  按钮可见、地址文案正确；不带 define → 断言预填本地、按钮隐藏。旧实现下前三条必红
  （变异验证：还原旧写法 3 红 3 绿）。
- 发行构建口径（务必照此打包，否则等于自部署版）：
  BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn npm run release

## 历史变更（v1.0.4）
- 隐私红线修正：线上地址改为**构建期注入**（BRIDGE_ONLINE_SERVER），源码与开源构建里
  不再出现任何厂商域名。默认构建（自部署）弹窗预填本地 localhost:8204，绝不把客户私信
  指向厂商云端；只有官方 SaaS 发行包在构建时注入云端地址，弹窗才默认线上。
- 弹窗「线上」按钮在未注入时隐藏，DOM 里也不渲染厂商域名；「线上/本地」标签域名字样
  改为运行时填充，开源用户看到的是一份干净的私有化产物。
- 单测补隐私腿：未注入 ⇒ constants.online 为空串、popup 默认=本地、线上按钮隐藏。
  注：注入实现（process.env.X）在扩展运行时失效，已在 1.0.5 修复。

## 历史变更（v1.0.3）
- 服务端地址默认线上：弹窗「服务端地址」未配置时预填云端地址 https://workapi.xapptool.cn
  （placeholder 同步），从平台下载安装包的租户开箱即连云端，不再需要手敲地址
- 地址两个快捷提示：弹窗新增「线上 / 本地」两个一键填入按钮（线上=workapi.xapptool.cn，
  本地=localhost:8204），自部署用户点「本地」即切回本机地址
- 线上地址单源：新增 constants.ONLINE_USER_SERVER（源 deploy/deploy.conf USER_API_URL 域名族），
  网页端「桥接扩展安装包」安装步骤按部署形态展示地址 + 复制按钮（直接引同一份 constants，
  两处展示永不漂移；SaaS 显示线上地址，私域显示本部署地址，不把云端地址误导给自部署租户）
- 运行期回退语义不变：未配置时 background 的兜底仍是 localhost:8204（DEFAULT_USER_SERVER
  未动），离线自部署的存量行为零影响

## 历史变更（v1.0.1）
- 修复同一条 AI 回复被反复重复发送：多个私信标签页同时开着时，下行发送改为按消息加锁串行，
  并在锁内重新读取已发送缓存，其他标签页已发过的消息不再重发（仍会 ack，服务端不会无限重推）
- 修复已发送缓存被互相覆盖：写回前先并入存储里其他标签页的条目，去重记录不再被单个标签页清空
- 目标会话确实不存在时（如客户会话已被平台移除）改为 ack 终态 failed，服务端停止重推；
  页面处于后台不可见状态时不 ack（后台标签页可能渲染不出会话列表，避免误判丢消息）
- 消息抽取：整段聊天线程被误当成「一条消息」时会向下拆成多条气泡（单条长消息不会被拆碎），
  拆不动且超过 4000 字的项按不可信气泡丢弃——此前整段线程会被反复当成新消息抓回并重复回复
- 会话 ID 清洗：剥掉平台 DOM 里混入的 MM/DD 日期串，避免同一会话被判成多个会话

## 包含能力
- 三平台私信桥接：抖音 / 小红书 / TikTok（网页版）
- 实时上行：客户私信 -> user-server（触发 AI）
- 历史回填：页面加载/会话切换时存量消息仅落库，不触发 AI（防回环）
- 下行回写：user-server AI 回复 -> 回写网页私信输入框并发送
- 拟人限速风控：最小间隔 + 令牌桶 + 会话冷却 + 相同文案去重
- 多用户/多会话：按 (channel, account, conversation) 隔离路由与历史

## 安装（本机 Chrome）
1. 打开 chrome://extensions
2. 开启「开发者模式」
3. 「加载已解压的扩展程序」-> 选择解压后的 dist 目录
4. 点击工具栏图标 -> 填写 user-server 地址（如 http://localhost:8204）-> 保存
5. 打开任一平台私信页 -> 点「自检当前私信页」按 bridge.md §10 真机校准清单核对

## 验证清单
- [ ] 客户发消息后 user-server 收到 inbound_message（AI 触发）
- [ ] AI 回复回写到网页并出现在对话框（outbound_reply）
- [ ] 刷新/切换会话后历史进入 user-server（history 帧，direction=inbound/outbound）
- [ ] 连续高频消息被限速拦截（popup / 后台日志可见 reason）
- [ ] 三平台（抖音 / 小红书 / TikTok / 闲鱼 / 快手）选择器真机校准通过
