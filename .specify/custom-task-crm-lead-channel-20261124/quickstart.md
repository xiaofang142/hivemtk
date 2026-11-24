# Quickstart: 四大部分定制任务验证指南

**前提**
- 内层仓 `hivemtk/`；迁移已执行到本地测试库。
- 测试库密码显式外导：`export POSTGRES_TEST_PASSWORD=...`（否则 `t.Skipf` 假绿）。
- bridge/TG hook/三方 API 测试客户端已升级为新识别码（一次性替换，无兼容期）。

## 1. 地基（第四部分）
1. `go test ./user-server/internal/service/... -run Channel -count=1` —— 渠道账号/识别码/自他识别单元测试通过并贴输出。
2. 调 `POST /api/manage/channels` 新增预设渠道 → `GET /api/manage/channels` 确认 enabled。
3. 调 `POST /api/manage/channel-accounts` 新增账号并绑定两类智能体 → `GET /api/manage/channel-accounts/options` 确认出现在统一列表。
4. 调 `POST /api/manage/channel-identify-codes` 生成识别码 → 用 bridge/hook/API 各发一条消息 → 确认统一收件箱按识别码归属到正确账号；用旧 bartoken 发送应失败。

## 2. CRM（第一部分）
1. 构造两个账号的重复客户 → `POST /api/manage/crm/customers/dedupe` 返回候选 → `merge` 合并 → `GET .../operation-logs` 见审计。
2. 员工 A 登录 → `GET /api/manage/crm/customers` 仅见权限内数据；越权 ID 详情返回 403。
3. `GET /api/manage/crm/customers/:id/360` 见完整链路；`GET /api/manage/crm/resources/search` 跨账号行业命中。
4. `GET /api/manage/crm/customers/:id/ai-profile` 每条建议附 evidence。

## 3. 线索（第二部分）
1. 为渠道 A 的账号 A1、A2 配不同规则与通知人。
2. 注入含关键词/语义/仅 LLM 可判的多轮对话 → `GET /api/manage/leads/judgements/:id` 见三路 hits + 二次 LLM 结论 + 上下文消息 ID。
3. 制造重复命中 → 确认防抖后只归档一条 → `GET /api/manage/leads/clues` 见归档含用户信息。
4. 通知按「账号维护人→渠道维护人→租户默认」送达，delivery_records 有结果。

## 4. 智能体剥离（第三部分）
1. 账号仅启用线索发现 → 对话后产生归档、无自动回复下发。
2. 账号仅启用自动回复 → 产生拟人工回复/转人工、无线索归档。
3. 两者同启 → 自动回复写入的转人工备注可被线索判定作为上下文引用。

## 5. 全量验证
- `go build ./...`、`go vet`、`gofmt -l` 干净。
- `python3 scripts/api_verify_full.py` 三端验证（入参/返回/预期/DB）全过。
- UI 页面用 ego-browser 逐元素显式点验（规则 -1），每页留快照/截图证据。

> 本指南为验证脚本说明，不含实现代码；实现细节见 tasks.md 与 data-model.md。
