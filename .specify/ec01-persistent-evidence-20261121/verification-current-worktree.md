# EC-01 当前工作树独立验收（2026-11-21）

结论：本地定向验收通过，EC-01 整体仍未完成；生产状态保持 `pending_live_validation`。本轮未修改产品代码，未提交、推送、部署或发出平台业务请求。

## 本轮实跑

- `go build ./...`、`go vet ./...`：退出码 0。
- 第一组 `go test -race -count=1`：capregistry/repository/service/migration/migrations/connector 共 5 包，21 个顶层测试、15 个子测试通过，0 失败、0 跳过。包括 15 场景 proof 隔离、租户/动作精确读取、单连接历史队列、撤权和迁移幂等。
- 第二组 `go test -race -count=1`：service/router/connector/bridge/channelgw 共 5 包，32 个顶层测试、305 个子测试通过，0 失败、0 跳过。包括管理员诊断权限与安全投影、提交拒绝零副作用、历史队列分页/错误回滚/单一认领赢家。
- `python3 scripts/check-layer-boundary.py`：扫描 1631 个非测试 Go 文件，0 跨层违规、0 基线豁免。
- `git diff --check`：退出码 0。
- 本轮没有执行全量 lint、全量 test-go、make audit 或前端/UI 交互测试，不以先前记录替代。

原始测试输出位于本目录 evidence/ 下。

## 生产与试点资源核查

只读查询本地及 mtk-prod 的 PostgreSQL 公共 schema，`merchant_connections`、`capability_evidences`、`capability_source_bindings` 均返回 0 张表。这只证明所查询数据库中这些台账表尚不存在，不据此断言所有外部平台均无授权。

生产桥接账号按渠道聚合存在 bilibili/douyin/weibo/xianyu/xiaohongshu；其中 douyin 是内容产品，不能继承 douyin_shop 的交易权限。集成账号聚合存在 ecomm 和 xiaoshouyi，但通用标签不证明真实店铺、提供方契约、scope 或动作级运行证据。本轮没有读取或输出 API key、token、secret。

生产装配 `internal/router/merchant_connection_routes.go:14` 仍以 nil providers 构造商户连接服务。默认持久裁决器没有生产 proof verifier；generic connector 的实际执行凭证尚无已验证连接修订绑定。当前仅能拒绝，不能通过测试夹具或数据库布尔值提升为生产可用。

使用 ego-browser 复用既有登录态访问后台配置入口，快照显示“欢迎登录系统”。未尝试登录、清 Cookie 或绕过认证。快照位于 evidence/live-resource-snapshot.txt。

## 未关闭的实际条件

需有可核验的试点租户/店铺和授权引用、当前提供方正式接入契约及可用服务端凭证配置，以及允许验证的动作/测试资源，才能完成生产提供方接线、受控真实采证、执行凭证修订绑定与逐格验收。不得把当前泛电商配置或内容抖音账号推定为抖店授权。

依未来任务清单 EC-01 行的串行约束，EC-01 未关闭前不推进 EC-02。保留已有工作树改动，不合并或提交其他会话工作。
