# 访客隐私告知同意闸（embed-sdk）

来源：2026-11-16 审计 P2-5。embed-sdk（ADR-011）目前直接加载聊天 iframe，
访客在部署方站点上的会话/消息收集没有任何告知-同意钩子，PIPL 告知-同意义务
完全落在部署方且无工具支撑。本 spec 给 SDK 加**可配置**的同意闸。

## 决策与边界

同意层做在 **SDK 侧、iframe 加载之前**：iframe 的 src 指向
`/chat/embed/:channel_ref`，iframe 页面加载即开始收集（建访客会话）——
所以闸的位置是「同意后才允许 `IframePanel.show()` 首次创建 iframe」，
不是「iframe 内首条消息前」（那需要 user-web embed 页配合，且该目录有并行
泳道在途改动）。SDK 是薄壳、原生 JS、运行时零依赖（ADR-011），同意层不引入
任何依赖、不改 iframe-panel.js。

不做的（Deviations 挂账）：
- 管理端「渠道开启页已知悉平台规则勾选留痕」：`user-web/src/i18n/locales/*`
  有并行泳道在途改动，本轮不碰 user-web/src，单独开任务。
- iframe 内首条消息前二次确认：同上，属 embed 页侧增强，不阻塞本 spec。

## 要求

- FR001 配置项 `privacyNotice`（对象，缺省 null = 关闭，存量零行为变更）：
  `{ required?: boolean, text?: string, policyUrl?: string, version?: string }`。
  required 缺省 false；version 缺省 '1'。data-* 支持布尔简写
  `data-privacy-required="true"`（其余字段经 `window.MarketingChatWidgetConfig` 传）。
- FR002 required=true 且未同意（或已存版本 ≠ 当前 version）时：浮标点击打开
  → 只渲染同意层（文案 + 可选政策链接 + 同意/拒绝按钮），**不创建 iframe、
  不发任何网络请求**；同意后写 localStorage
  （键 = `<visitorIdKey>:privacy_consent`，值 = 已同意 version），同版本不再弹。
- FR003 拒绝 → 关闭聊天窗（等效 close()），不写任何存储；下次打开重新询问。
- FR004 事件：`onPrivacyConsent` 回调（参数 `{ accepted: boolean }`）；
  同意层打开/关闭不复用 onOpen/onClose 语义之外的新事件。
- FR005 文案可配 `text`；未配时按 `lang`（zh-CN/en-US）给内置默认文案与按钮文字；
  `policyUrl` 非空时渲染 `<a target="_blank" rel="noopener noreferrer">`。
- FR006 同意层样式用 `mcw-consent` 前缀类，随 styles.css 构建；z-index 跟随
  实例 zIndex + 1，不与浮标/面板抢层。

## 验收

- config：privacyNotice 缺省 null；required 简写解析；非法形状降级为关闭。
- 闸：required=true + 未同意 ⇒ open() 后 iframe 不创建（panel.iframe === null）；
  同意 ⇒ 存储写入 + iframe 创建；拒绝 ⇒ 无存储且窗关闭；version 变更 ⇒ 重新询问。
- 关闭：required=false（或 privacyNotice 缺省）⇒ 行为与现状逐字节一致
  （open 直接 show，无同意层 DOM）。
- 测试全部进 embed-sdk 既有 node 原生测试（unit.test.mjs），CI ci-embed-sdk 全绿。
