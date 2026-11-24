# Bug Assessment: channel dispatch swallows repository query failures

- **Slug**: channel-dispatch-query-errors-20261125
- **Created**: 2026-11-25T01:05:00+08:00
- **Source**: pasted text — requirements.md §13 CHANNEL-THREE-PATH / AUDIT-01
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

> `PickAccount` 对 channel/conv 查询错误仍 swallow；仓储不可用会退化为后续选号，需确认是否应 fail-visible。

冻结源码：`934825c2d6b804e79c3bf144cbe25dbcc913007e`。

## Symptom

`ChannelDispatchService.PickAccount` 在渠道解析、粘性会话查询、粘性/首选账号查询失败时丢弃错误，继续 preferred 或 round-robin。数据库瞬断或仓储故障可能把本应失败/重试的发送改派到另一账号，而不是 fail-visible。

预期：仅“记录不存在”可以继续后续选号策略；任何运行错误必须原样上抛（可 `%w` 包装），且不得返回账号。

## Reproduction

1. 构造 `ChannelDispatchService`，让 `GetByTenantAndCode` 返回注入错误；以 channel code、无 `ChannelID` 调用 `PickAccount`。
2. 当前返回 `channel required`，注入错误丢失。
3. 给定有效 `ChannelID` 与 `ConversationID`，让 `FindLatestByPlatformAndPeer` 返回注入错误，同时仓储中仍有可用账号。
4. 当前吞掉会话查询错误并返回 `round_robin` 账号。
5. 对 sticky/preferred 账号 `GetByID` 注入运行错误，当前同样降级到 round-robin。

## Suspected Code Paths

- `user-server/internal/service/channel_dispatch.go:60` — channel 查询错误被 `err == nil` 条件吞掉。
- `user-server/internal/service/channel_dispatch.go:75` — conversation 查询错误被吞；未区分 `gorm.ErrRecordNotFound` 与运行错误。
- `user-server/internal/service/channel_dispatch.go:80` — sticky account 查询错误被 `_` 丢弃。
- `user-server/internal/service/channel_dispatch.go:89` — preferred account 查询错误被 `_` 丢弃。
- `user-server/internal/repository/channel.go:106` — 未命中返回 `(nil, nil)`，运行错误返回 error，契约明确。
- `user-server/internal/repository/channel_account.go:74` — 未命中返回 `(nil, nil)`，运行错误返回 error，契约明确。
- `user-server/internal/repository/message_hub_inbox_conversation.go:130` — 未命中返回 `gorm.ErrRecordNotFound`，运行错误返回 error。

## Root Cause Hypothesis

高置信度：调用方把“查询没命中”和“查询失败”混为一谈，用 `err == nil && row != nil` 或 `_` 丢弃错误。仓储层契约本身正确，缺陷在 service 的错误分流。

## Proposed Remediation

**Preferred**：

1. channel 查询：`err != nil` 立即 `%w` 返回；`ch == nil` 才继续走 `channel required`。
2. conversation 查询：仅 `errors.Is(err, gorm.ErrRecordNotFound)` 允许继续；其他 error 上抛。
3. sticky/preferred account 查询：`err != nil` 上抛；`nil` 或状态/渠道不匹配才继续后续策略。
4. 不改变 tenant/channel 过滤、sticky→preferred→round-robin 优先级与 round-robin 算法。

**Alternatives**：

- 把所有查询错误都当作“无粘性”继续：拒绝，会把数据库故障变成错账号发送。
- 在仓储层把 not-found 也改成 error：会破坏现有仓储契约与大量调用方，不做。

**Files likely to change**:

- `user-server/internal/service/channel_dispatch.go`
- `user-server/internal/service/channel_dispatch_query_errors_test.go`

**Tests to add or update**:

- channel 查询运行错误必须上抛且零账号返回。
- conversation 查询运行错误必须上抛；record-not-found 仍可降级。
- sticky account 查询运行错误必须上抛，不得 round-robin。
- preferred account 查询运行错误必须上抛，不得 round-robin。
- 回归 sticky/preferred/round-robin 正常路径与跨租户/跨渠道/禁用账号过滤。

## Risks & Considerations

- 调用方会把数据库瞬断视为发送失败而不是静默改派账号，这是预期 fail-visible 行为。
- 必须保留 `gorm.ErrRecordNotFound` 的降级语义，否则首次会话无法选号。
- 错误消息使用 `%w`，保留 `errors.Is` 能力，不泄露连接串。

## Open Questions

- 无。requirements 已明确要求审查查询错误 swallow；本项为可复现真缺陷。
