# Bug Fix: 营销流程短信与邮件动作接入统一发送闸口

- **Slug**: marketing-flow-send-gate-20261121
- **Fixed**: 2026-11-21
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

营销流程的 `send_sms`/`send_email` 动作现在与 AI 工具、主动触达、批量管线走同一条发送前判定链：DNC → W-1 审批 → E1 合规（静默时段/日上限），判定通过才允许真实投递；未装配判定实例 = fail-closed 拒发。流程撤回/暂停对在途执行也生效（发送时点复核状态）。动作层不再直接接触 SMTP。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/content/service/marketing_flow_send_gate.go` | added | 邮件注入点 `SetEmailSender` + 配置结构、流程引用 ctx 传递、`ensureFlowActiveAtSend` 状态栅栏、邮件配置解析（校验口径逐字保留） |
| `user-server/internal/content/service/marketing_flow_action.go` | modified | `sendActionSendSms` 前置状态栅栏；`sendActionSendEmail` 改为栅栏 + 注入点调用，删除直连 SMTP；`net/smtp` import 移除 |
| `user-server/internal/content/service/marketing_flow.go` | modified | `executeFlow` 把流程引用挂上 ctx（`WithMarketingFlowRef`） |
| `user-server/internal/service/marketing_flow_reach_gate.go` | added | 闸门版发送器注册（`init()`）：`enforceMarketingFlowOutboundGates`（DNC→审批→合规唯一出口）+ 传输替身注入点；SMTP 传输从动作层原样搬移 |
| `user-server/internal/service/marketing_flow_sms_init.go` | modified | 移除 `init()` 裸注册与多余 import；仅保留 `lazySendSms` 真实通道 |
| `user-server/internal/app/marketing_flow_gate_wiring.go` | added | `AttachMarketingFlowReachGate(db)`：构造判定实例 → `AttachReachGate` 装审批门 → 注入；db nil 只留痕（发送器随后 fail-closed） |
| `user-server/cmd/api/main.go` | modified | router.Setup 之后（InitCollectionRuntime 旁）挂营销流程闸门，顺序约束注释同催收/挽回腿 |

## Tests Added or Updated

- `user-server/internal/service/marketing_flow_send_gate_test.go`（新增，6 用例）：未装配 fail-closed、审批拒绝、DNC 命中、静默时段、日上限配额 ⇒ 零传输调用；放行路径恰好一次。
- `user-server/internal/content/service/marketing_flow_send_gate_test.go`（新增，4 用例）：暂停/停用在途执行零外发且执行 failed、active 对照照常发送、邮件未注册 fail-closed、注册后收到解析配置。

## Local Verification

- Commands run（2026-11-21，读数与日志见 test.md/gates.json）：
  - 判定链矩阵 6 PASS（green-gate.log）+ race 通过（race-gate.log）。
  - content 层栅栏 4 PASS（green-content.log）+ race 通过（race-content.log）。
  - 既有回归：`TestMarketingFlow*` 45 PASS / 0 SKIP / 0 FAIL（regression-content.log，专用快测实例 127.0.0.1:8217）；触达/合规/线索租户 18 PASS（regression-reach.log）。
  - build / vet / lint（0 issues，修一次 gofmt 后）/ 层边界 / 架构 / naked-goroutine / bare-go / GORM-only 全 rc=0（gates.json）。
  - 改动文件 gofmt 无输出、`git diff --check` rc=0。
- Manual checks: 全部为内存替身与测试库；未连接生产库、未发送真实短信/邮件；未提交/未推送/未部署。

## Deviations from Assessment

无（装配点、fail-closed 语义、传输搬移口径均按 assessment 落地）。

## Follow-ups

- 邮件动作现经统一闸口但传输仍为节点自带 SMTP 凭据直投：是否收敛到统一邮件服务（SMTP 账号管理/退订/队列）属产品决策，不在 FA-20 范围（与 FA-EMAIL-01/02/03 各自独立）。
- 判定链拒绝目前使执行 failed，自动退避/重排属流程引擎后续能力，不在本项范围。
