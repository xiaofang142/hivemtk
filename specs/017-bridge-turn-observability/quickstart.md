# 验证入口

- `go test ./internal/pkg/tracing -count=1`：时间/脱敏/作用域基础；数据库用专用POSTGRES_TEST环境，密码由.env读取且不打印。
- `go test ./internal/service ./internal/bridge ./internal/monitor ./internal/service/trace_learning -count=1`：在T2/T3完成后运行；无业务PG写。
- `npm run test -- tests/unit/traceTiming.test.js`：在user-web执行T3格式/归并验证。
- 本特性脚本环境：`SPECIFY_FEATURE_DIRECTORY=specs/017-bridge-turn-observability SPECIFY_FEATURE_NO_PERSIST=1`。不覆盖共享feature.json。
- 全部任务完成前不发布；真实平台动作仅明确测试对象、ego逐元素，缺测不补造。
