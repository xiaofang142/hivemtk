# Test: 线索历史与节流账号范围（LM-20261125-01）

- **Date**: 2026-11-25
- **Result**: 主智能体复验 scoped GREEN（真实 PG）

## Evidence

- `go test ./internal/service ./internal/repository -run 'TestLeadHistory_|TestMessageHubHistory_|TestLeadReact' -count=1 -p 1 -parallel 1`
  - `ok hivemtk-user/internal/service 2.940s`
  - `ok hivemtk-user/internal/repository 0.970s`
- 覆盖：规范账号别名、按账号防抖、真实 PG 账号隔离历史、sender 兜底、不完整作用域拒绝、ReAct 仓储范围。
- 全量 service 回归仍有 Bridge/Webhook 基线失败，非本 lane 引入。
- 未做真机/发布/外发/UI 点验、未提交。
