# 扩展64项告警评估

Verdict: valid / low。

## 证据与决策

完整扩展ESLint已返回零错误、64告警：45项未使用绑定、13项可使用const、6项多余正则转义。此次仅消除这些已定位站点，不删除初始化、导入副作用或函数，不改取消、鉴权、消息身份、发送互斥、限流或重试预算，也不关闭规则或执行历史UI跑批。

- 未使用catch参数移除；其它未使用绑定显式改为唯一的下划线名称，同一词法绑定引用同步修改，保留导入名和对象属性键。
- const转换限于已初始化且后续无写入的声明，保持对象内容可变。
- 正则移除转义前后解析树必须等价，保持字符集合和匹配语义。
- 逐文件核对允许变换后的AST等价及实际ESLint零告警，再应用补丁和运行完整扩展测试。

## Files likely to change

- `user-web/bridge/scripts/build.mjs`
- `user-web/bridge/src/background/index.js`、`sse-client.js`
- `user-web/bridge/src/channels/douyin.js`、`kuaishou.js`、`tiktok.js`、`xhs.js`、`xianyu.js`
- `user-web/bridge/src/content/common.js`
- `user-web/bridge/src/core/channel-adapter.js`、`dom.js`、`downlink.js`、`fallback.js`、`http-ingest.js`、`humanize.js`、`logger.js`、`selector-ai.js`、`selector-engine.js`、`sse-fetch-client.js`、`uplink.js`
- `user-web/bridge/src/popup/accounts.js`、`index.js`
- `user-web/bridge/test/adapter-outbound-queue.test.js`、`downlink.test.js`
- 本目录fix/test及主进度。AST和正则验证脚本及证据仅写入本轮临时目录。

## 验证与限制

当前64告警为静态失败基线。不得用改规则、扩大ignore或全仓--fix代替修复。后端正式Go正在验证，此项不修改后端或启动另一组PG测试。通过只证明该重构范围和受控回归，不代表扩展已安装、生产消息已送达。
