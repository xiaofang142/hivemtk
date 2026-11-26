# Quickstart: 四大部分定制任务验证指南

**运行前提**
- 本文所有仓库相对路径均从工作区下的产品仓 `hivemtk-user/` 执行；工作区根不是 git 仓。
- Go/Postgres 测试只使用专用快测库 `127.0.0.1:8217`。`POSTGRES_TEST_PASSWORD` 从本地环境注入；不要把口令写入命令历史或文档。
- `api`、`crm`、`sc001` 会通过 `HIVEMTK_BASE` 写入/删除 CRM 测试数据；运行前确认目标服务连接同一个专用 8217 测试库。三个子命令必须串行运行，禁止并行写同一测试库。DB 清理依赖脚本的 finally 路径，但运行后仍须查看清理报告，并把原始输出归档到当次取证目录。
- `HIVEMTK_ADMIN_PASS` 必须由本地环境注入。SC-001 默认不调用外部 AI；不要设置 `HIVEMTK_ALLOW_EXTERNAL_AI=1`，除非已明确授权外发。
- 命令只运行显式选择的子命令；不得运行 `all`/`chain` 代替隔离验收。UI 验证遵守仓库规则 -1，逐元素操作并留证据。

## 1. 地基（第四部分）

```bash
# 当前目录应为工作区下的产品仓 hivemtk-user/；不要再重复 cd hivemtk-user
export POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8217
```

1. 按仓库测试规范从本地环境提供 `POSTGRES_TEST_PASSWORD`，运行 `cd user-server && go test ./internal/service/... -run Channel -count=1`，保存完整输出；不得把跳过用例计为通过。
2. 在隔离测试租户调用 `GET/POST /api/channels`，确认预设渠道列表与新增结果。
3. 调用 `GET/POST /api/channel-accounts` 与 `/api/channel-accounts/options`，核对账号及统一下拉列表。
4. 调用 `POST /api/identify-codes/:channel_id/rotate` 重置识别码，并通过获授权的 bridge/hook/API 测试客户端验证；不得向真实客户或渠道外发。
5. 需要通过 bridge 注入测试消息时，使用合成数据并设置 `expect_reply:false`，避免触发自动回复外发。

## 2. CRM（第一部分）

使用隔离测试租户和测试员工，留存请求、响应、数据库核对及清理结果：

1. 创建不同账号的重复客户；调用 `POST /api/manage/crm/dedupe/scan`、`POST /api/manage/crm/merge`，再核对 `GET /api/manage/crm/merge/audit`。
   扫描输入须显式设置 `same_account_only: false`，使跨账号用例进入候选范围。
2. 对照员工数据范围核对 `GET /api/manage/crm/customers` 与 `GET /api/manage/crm/customers/:id`；另验证未授权访问按预期拒绝。
3. 核对 `GET /api/manage/crm/360/:id` 和 `GET /api/manage/crm/cross-industry`。
4. 在不触发外部 AI 的配置下验证 `POST /api/manage/crm/ai/analyze` 的禁用/降级行为；只有明确授权且目标为本地 mock 时，才启用外部 AI 调用。
5. 先确认测试租户没有启用的通知路由，再用合成 `clue_id` 调用 `POST /api/manage/crm/clue/archive`，核对关联客户与 `clue_archive_link_customer` 审计；`notified=true` 不等于真实送达。

## 3. 线索（第二部分）

1. 在隔离测试租户为不同渠道账号配置不同规则与通知路由。
2. 使用测试消息数据验证 `GET /api/lead-mining/judgements/:judgement_id`；线索 hits 核对 `GET /api/manage/leads/judgements/:judgement_id/hits`。
3. 核对线索列表 `GET /api/clues/list`，验证重复命中防抖、归档信息及关联客户。
4. 通知验证仅使用本地替身或明确获授权的测试收件端，并核对 delivery records；不要向真实收件人发送测试消息。

## 4. 智能体剥离（第三部分）

1. 在本地测试配置中仅启用线索发现，检查归档且不产生自动回复外发。
2. 仅启用自动回复时用本地 mock 检查拟人工回复/转人工路径，且不产生线索归档。
3. 两者同时启用时验证转人工备注可作为线索上下文；全程不得调用未授权的外部 AI 或真实渠道。

## 5. 显式隔离验证命令

从 `hivemtk-user/` 执行；API 服务地址按本地隔离环境显式提供，且必须连接上述 8217 测试库：

```bash
: "${POSTGRES_PASSWORD:?请从本地环境注入}"
: "${HIVEMTK_ADMIN_PASS:?请从本地环境注入}"
export HIVEMTK_DB_PORT=8217
export HIVEMTK_DB_PASSWORD="$POSTGRES_PASSWORD"
export POSTGRES_TEST_PASSWORD="$POSTGRES_PASSWORD"
export HIVEMTK_BASE=http://127.0.0.1:8204
mkdir -p "${TMPDIR:-/tmp}/opencode"
export HIVEMTK_EVIDENCE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/opencode/item2-sc001.XXXXXX")"
python3 scripts/api_verify_full.py api
python3 scripts/api_verify_full.py crm
python3 scripts/api_verify_full.py sc001
```

每次仅选择一个子命令，逐项核对【入参】【返回】【预期】【DB】及最终清理结果。API 默认地址为 `http://localhost:8204`；上例用本机隔离服务，只有确认该服务连接测试库后才能执行。不得仅因 API 可访问就推断其连接了 8217。

全量编译与静态检查：进入 `hivemtk-user/user-server/` 后运行 `go build ./...`、`go vet ./...` 和 `gofmt -l .`，记录各自输出。UI 页面按规则 -1 逐元素点验，每页保留快照、截图、toast 或接口状态证据。

> 本指南列出验证步骤，不代表任何步骤已经实跑或通过；完成情况只按 `tasks.md` 勾选和对应原始证据记录。
