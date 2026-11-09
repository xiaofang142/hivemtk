# Bug Assessment: AV-01/02 测试库配置保护

- **Slug**: testdb-config-safety
- **Created**: 2026-11-06
- **Source**: `docs/未完成清单.md` AV-01/02；用户授权开始编码、测试、交付
- **Verdict**: valid
- **Severity**: high

## Report

测试入口必须拒绝业务库配置，显式端口不可达或认证失败不得回落其他实例；日志不得输出凭证。保持用户槽位模型、平台串行共享库模型。

## Symptom and Reproduction

源码确认用户端显式测试端口仍参与多端口探测；库名未经校验即拼进进程库 DDL；URL DSN 被正则替换而未真正切换数据库；两端缺密码回落硬编码口令。现有 masker 仅覆盖首个未引用 keyword 密码。只读 TCP 核验：用户专用容器 `mtk-postgres-test`，宿主 8217，独立卷 `hivemtk_mtk_user_pg_test_data`；`postgres` 认证 SELECT 1 成功，admin 有 CREATEDB。平台宿主 8201、`platform_db_test` SELECT 1 成功。未改角色、卷、业务数据库。危险配置仅用纯解析/子进程测试复现，不执行业务库 DDL。

## Root Cause

测试配置没有统一的解析与验证门，端点探测、基名和 DSN 替换各自解释配置；Makefile fast 档未检查 docker 凭证读取失败，也未同步用户名。置信度高（源码直接证据）；无数据损失实证。

## Proposed Remediation

使用已有 pgx 解析 keyword/URL DSN；用户基名仅 `user_db_test`、平台仅 `platform_db_test`。默认用户实例为专用 8217，显式测试端口/DSN 不回落业务端口。缺失口令报配置错误，保留显式 POSTGRES_TEST_PASSWORD 优先、POSTGRES_PASSWORD 兼容来源，显式 DSN 最高优先。数据库切换使用 URL path 或追加正确转义的 keyword；仅已验证基名生成 slot/PID。日志仅显示身份摘要，不透传原始解析错误中的密钥。fast 入口凭证读取失败即终止，用户名同步。不更改生产配置、角色或卷，不统一平台为槽位模型。

**Files likely to change**:
- `user-server/internal/pkg/testutil/testdb.go`
- `user-server/internal/pkg/testutil/testdb_config.go`
- `user-server/internal/pkg/testutil/testdb_config_test.go`
- `Makefile`
- `docker-compose.test.yml`（仅测试初始库/健康检查）
- 平台独立切片：`platform-server/internal/testutil/testdb.go`、`testdb_config.go`、`testdb_config_test.go`，使用该仓已有 pgx 依赖。

**Tests**: 显式端口不回退；URL/keyword 同义、正确切换维护/槽位库；重复 dbname、引用/注入/业务库名拒绝；缺密码/坏端口/解析错误脱敏；认证失败不跳过；两进程槽位互斥与锁释放；两端只读 SELECT 1 预检。Makefile fast 失败读取零 go-test 调用。

## Risks and Scope

默认用户测试端口改为专用 8217；原 CI 显式 POSTGRES_TEST_* 仍有效。端点/名字白名单不能证明任意远程实例无业务数据，仍需操作者事先批准实例。本切片不启用生产外发、不推进渠道/UI、不覆盖在途改动。平台共享测试库仅 -p 1。全量失败按实际归属报告，不覆盖其他会话文件以造绿。
