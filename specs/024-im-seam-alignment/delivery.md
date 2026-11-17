# 024 IM 接入接缝对齐 — 交付记录（delivery）

> 交付日期：2026-11-18。施工源：本目录 spec.md / plan.md / tasks.md（45 任务 / 6 批次）。
> 范围口径：本 spec 只交付「接缝」，不以任何平台上线为完工判据（平台接入在 025）。

## 一、批次交付总览

| 批次 | 任务 | 交付提交 | 证据 |
|---|---|---|---|
| B1 出站接缝 | T101–T112 | `0351830c`（注册表+8 sender+收口）+ 本批 `T111/T112` 测试 | `sendOutbound` 实测 126 行（≤150）、`switch channel` 0 处；`outbound_registry.go` + 8×`outbound_sender_*.go` |
| B2 事件类型 | T201–T211 | `9aa50f16`/`b0eff8ba`/`cbfa1bd0` | 迁移 068/069、`EventType` 分流、交互回调上下文 |
| B3 事件路由 | T301–T305 | `eb09e028` | 迁移 066、`channel_route_engine`（无规则=旧行为） |
| B4 渲染+身份键 | T401–T404、T501–T502 | `2dda6227`/`adca7423` | 迁移 065、`RenderForChannel` 单点收口 |
| B5 生命周期读侧 | T601–T606 | `0e1c8d8a` | 迁移 067、snoozed/待首响/首响指标 + 前端筛选（T606 于本批回勾） |
| B6 门禁与文档收口 | T1201–T1205 | 本批 | 见下 §二 |

## 二、B6 收口明细（2026-11-18）

- **T1201 接入触点自动化校验**：`scripts/check_im_touchpoints.py`（六触点矩阵：常量/manifest/账号表/入站分支/sender 注册/client 包，全部从源码符号提取）+ CI 作业 `.github/workflows/im-touchpoints.yml`。验收：正例绿；负例（注释掉 telegram 注册）→ 红并列出「渠道+缺失项」。
- **T1202 一致性测试**：`internal/capregistry/consistency_test.go`——IM 能力条目（dm.receive/dm.send/webhook）逐条校验 `channelcaps.Known`；sms/email/ima/webhttp 等非消息能力豁免（无消息 manifest 是正确定位）。
- **T1203 文档与索引**：新增 `docs/channel-onboarding.md`（六触点清单 + curl 冒烟）；`docs/INDEX.md` §四/§九、`specs/README.md` 状态行登记（024 已交付、025 前置就绪）。能力表 `channelcaps` 生成链路测试绿。
- **T1205 出站可观测三指标**：`outbound_send_failed_total{channel,category}`（挂 `markSendFailed`/`markSendFailedLogged` 两个失败闭包）、`outbound_text_overflow_total{channel}`（渲染溢出分支）、`route_rule_hit_total{tenant,target}`（路由命中点）；裸错误统一归 `unknown` 维度。验收测试 `TestOutboundFailureMetricDimensions`（同渠道 window_expired/unknown 分维计数）+ `TestRenderOverflowMetric`（溢出只计数不切分且照常投递）。
- **R5 不变量补上**（T101 二次审核条款，B1 时漏装）：`sendOutbound` 在 sender 返回后校验 `!sent && sendErr == nil` → 兜成 `unknown + 可重试` 失败；`TestSenderOutcomeMustBeExplicit` 钉住。
- **T112 行为冻结金样**：`outbound_golden_test.go` + `testdata/outbound_golden.json`——8 渠道 × 3 样例 = 24 条，捕获平台可见 `(method, path, body)` 序列 + 投递事实；平台端点经 Transport 层劫持、凭据行种测试库。冻结模式（不带 `UPDATE_GOLDEN`）两连绿。

## 三、T1204 全量门禁记录

| 门禁 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` | ✅ 绿 |
| `go test ./internal/service/ ./internal/channelbot/... ./internal/channelcaps/ ./internal/channelgw/... ./internal/router/ ./internal/capregistry/` | ✅ 六包绿（唯一例外见 §五） |
| `go test -race ./internal/service/ ./internal/channelbot/...` | channelbot ✅；service 修复一处测试假件竞态（`swapDeliverFollowAction` 无锁写）后 ✅ |
| `check-layer-boundary.py` | ✅ 0 违规（基线 0） |
| `check_model_migration.py` | ✅ 395/425 模型全注册 |
| `check-enum-consistency.sh` | ✅ 通过，2 条既有 warning（`system`/`intent_major`），无新增 |
| `check-naked-goroutine.py` | ✅ 0 hit |
| `check_im_touchpoints.py`（本轮新增） | ✅ 常量 17 / manifest 21 / sender 7 / 入站 7 / 账号表 7 / client 包 4 |

## 四、本批顺带修复的存量缺陷（规则 2：发现问题立即修复）

1. **勿扰窗测试装配失效（B1/B8 引入的时依赖缺陷）**：`DisableAIQuietHoursForTest` 原实现往全局配置桩塞无租户前缀的缓存键，而测试跑在 `tenant.ModeOn`+租户 ctx 下必然 miss——勿扰窗（默认 23–7 点）照常激活。装配 helper 的 `defer X()()` 又写在 setup helper 里，**helper 一返回就复位**，异步 AI 腿全程裸奔勿扰窗：**23:00–07:00 跑测试必红**（CS-06 三例、bridge follow 等），白天跑永不暴露。修复：helper 改走 `aiReplyQuietHoursFn` 替换缝（全租户无关单点），6 处 helper 内 `defer` 改挂 `t.Cleanup`。
2. **channelgw 静态闸与注册表化脱节**：`assertNoBridgeChannelCaseList` 仍要求 `webhook_outbound.go` 内出现 `bridgeOutboxCapable(channel)` 字面量（B1 前的住址），B1 搬迁后恒红。修复：能力查询断言迁到 `outbound_registry.go`，webhook_outbound 只保留「case 子句不得出现桥接常量」的防回潮断言（豁免注释行与函数定义行）。
3. **测试假件数据竞态**：`bridge_follow_test.go` 的动作行探针无锁写 `[]string`，`-race` 必报。修复：读写两端走 `followActionMu` + accessor。
4. **capregistry 一致性门落地时发现** `if false ||` 死条件与 `webhttp/ima` 平台无消息 manifest 属定位正确——前者在 wecom_integration.go（025 平台批重构时一并处理，不在接缝范围）。

## 五、诚实边界（不谎报）

- `internal/service` 包有 **3 条失败测试**：`TestIMEventEvidence_*`——来自**并行会话未提交**的 `inbox_ingress_event_evidence_test.go`（untracked，2026-11-17 23:20 生成，git 状态 `??`）。失败模式：ModeOn 下 varchar 租户列与 pgx 编码交互（`unable to encode 0x12 into text format`）。已验证与本批改动无关（无本批改动的基线同样红），归其作者会话处置，未纳入本批提交。
- T112 的「搬迁前 golden」窗口已过（B1 搬迁提交先于本批收口）：搬迁前后的等价性由**迁移前编写、断言零改动的既有测试**（channels_delivery_fix / bridge_sent / card_fallback / BF6 等）承载，金样冻结的是当前契约、防未来漂移。
- 错误类别映射的个别口径（whatsapp 131047→auth、feishu 230013→unknown+可重试）按现状冻结，若要细化归 025 平台批的渠道错误分类整理。

## 六、二次审核（二）：收口批对抗复核与修正（2026-11-18）

- **R1 金样顺序依赖（严重，已修）**：feishu/wecom/wechat api_err 样例依赖 ok_text 暖 token 缓存、
  qq msg_seq 跨样例递增 → 单样例 `-run` 必红。修：每样例独立 svc + DB token 清缓存 + qq 唯一 msg_id
  + 冻结比对改按标签（子集可校验）。验证：全量冻结绿 ×2，三样例单独 `-run` 绿。
- **R2 重放测试空断言（已修）**：`TestSendOutbound_DelayedReplayNoRetry` 改 DB-backed + 负对照
  （非重放入队 1 行证明夹具有牙，重放 0 新增）。
- **R3 触点脚本死代码（已修）**：`model_channel_constants` 双遍循环收敛为单遍。
- **R4 UPDATE_GOLDEN 部分覆写漏洞（当场触发，已修）**：`-run` 过滤下重写会把 golden 截断；
  加护栏「样例集不全拒绝重写」。
- 无行动确认项与留观项见 `review2.md`。
- 复核后回归：金样全量冻结 ×2、feishu/wecom/wechat api_err 单独 `-run`、
  大回归集（registry/parity/metrics/claim/dingtalk/bridge）、`-race` 子集——全绿。

## 七、遗留与去向

- 025 平台接入（wecomcs/OneBot/Slack/wecombot/custom）：B1+B2 前置已就绪，按 025 spec 排期。
- `webhttp/ima` 的粗细能力矩阵归位与 wecom_integration 死条件清理：归 025 首切片前的杂项。
