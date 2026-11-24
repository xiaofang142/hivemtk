# Bug Assessment: FA-25 iframe query

- Created: 2026-11-22
- Verdict: valid
- Severity: medium

## Root cause

embed-sdk/src/iframe-panel.js create 将 welcome/lang 写在 fragment 之前；接收端 user-web/src/views/chat/embed/Index.vue 只读 vue-router route.query。注释与实际地址矛盾。动态验证待新增真实 router 契约测试。

## Proposed remediation / Files likely to change

- embed-sdk/src/iframe-panel.js：将参数放入 #/chat/embed/:channelRef 后的 query，父 origin 校验保持不变。
- user-web/tests/unit/embed_sdk_route.test.js：直接加载 SDK 源码建立 iframe，通过真实 Vue Router 解析；覆盖 Unicode、空值、&/?/#/%、一次编码和 channel_ref。

保持服务端欢迎语优先级和所有无关配置。测试不发网络或消息。SDK 构建后跑全套，并跑真实 router 契约；实际外部 iframe 手工验收单独标明。
