# 实施计划

技术：Go服务内有界通知 broker、Controller流映射、原桥接认证；MV3后台fetch通知、原PollingLoop补拉。不新增数据表。

允许文件：
- 新 `user-server/internal/service/bridge_outbox_wake.go` 与同名测试。
- 新 `user-server/internal/controller/bridge_outbox_wake.go` 与同名测试。
- 修改 `user-server/internal/app/bridge_wiring.go`、`internal/router/router.go`、`internal/controller/bridge_capabilities.go`。
- 新 `user-web/bridge/src/background/outbox-wake.js`、`src/core/outbox-wake.js` 与对应测试。
- 修改 `src/background/sse-client.js`（增加新模块导入，旧投递接口保留）、`src/background/injector.js`（导出现有渠道映射）、`src/core/polling-loop.js`。
- 新/更新相关测试、本文档及总台账。保护他人在途 background/index.js 和 http-ingest.js，不覆盖。

步骤按 tasks.md；每项真实失败测试后最小实现，完整扩展与相关Go包回归，最后真实只读连接验证及未覆盖说明。
