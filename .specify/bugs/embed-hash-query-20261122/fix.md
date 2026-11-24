# Bug Fix: FA-25 iframe hash query

- Fixed: 2026-11-22
- Status: applied

iframe-panel.js将welcome/lang从fragment前移到`#/chat/embed/:channelRef?`后；保留mcwParent和origin白名单。新增user-web真实Vue Router契约测试，覆盖Unicode、`&/?/#/%/+`、空值、一次编码和channel_ref。

## Local verification

- 修复前：3/3 FAIL，route.query为空/undefined。
- 修复后：`npm test -- tests/unit/embed_sdk_route.test.js` 3/3 PASS。
- `embed-sdk` build成功；`npm run test:all` 116+62 PASS/0FAIL。

## Residual Risks

真实浏览器iframe欢迎语/语言渲染和服务端渠道欢迎语优先级仍需G-UI证据；本轮未操作真实页面。
