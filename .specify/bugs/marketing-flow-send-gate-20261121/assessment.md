# Bug Assessment: 营销流程短信与邮件动作必须经过统一发送闸口

- **Slug**: marketing-flow-send-gate-20261121
- **Created**: 2026-11-21
- **Source**: `docs/未完成任务清单.md` FA-20
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

`internal/content/service/marketing_flow_action.go:355–387` 直调 SMS sender，`internal/service/marketing_flow_sms_init.go:22–58` 直接接 `SmsService.SendSms`；后者仅有本通道的时间/号码/短信退订检查。邮件动作 `marketing_flow_action.go:642–709` 直接 `smtp.SendMail`。两者没有经过统一的审批、地区行业策略及联系偏好判定；统一出口的约束见 `service/proactive_reach.go:308–332` 及实际 reach pipeline 装配。

## Symptom

营销流程（marketingFlow）的 `send_sms` / `send_email` 动作各有两条裸发通道：

1. **短信**：动作层经 `smsSenderFunc` 注入点直调 `lazySendSms` → `SmsService.SendSms`，仅有短信通道自身的时间/号码/退订检查，不经过 W-1 审批、E1 合规（静默时段/日上限）、DNC 全局退订。
2. **邮件**：动作层直接以节点 config 里的 SMTP 凭据 `smtp.PlainAuth` + `smtp.SendMail` 拨号，没有任何发送前判定。

而 AI 工具、主动触达（ReachByCustomer）、批量管线的外发都走 `ProactiveReachService.enforceOutboundGates`（W-1 审批 → E1 合规）+ DNC。营销流程是唯一不受统一外发纪律约束的出域路径。

## Reproduction

1. 注册一个计数发送器（或观察真实通道），构造含 `send_sms`/`send_email` 动作的合法流程并触发。
2. 命中 DNC 名单/审批拒绝/静默时段/日上限的收件人：改造前动作照常外发（短信仅靠通道内零散检查，邮件完全无检查）。
3. 流程在执行中途被 `PauseFlow`：改造前在途执行继续把剩余动作全部发完（状态检查只在 TriggerFlow 入口）。

## Root Cause Hypothesis

发送器注册点（`init()` 里的 `SetSmsSender(lazySendSms)`）把裸通道直接交给了动作层；邮件则根本没有注入点、把 SMTP 拨号写死在动作层。统一判定链在 `internal/service` 包（ProactiveReachService），而 content/service 不能反向 import（成环），所以两条通道都绕过了它。置信度：high（源码可确认）。

## Proposed Remediation

**Preferred**：把判定链装到发送器注册实现里（internal/service 层，与 ReachByCustomer 显式手机号/邮箱路径同序同源：DNC → enforceOutboundGates）：

1. content 层新增邮件注入点 `SetEmailSender`（与既有 `SetSmsSender` 同模式），邮件动作只负责解析/校验配置并调用注入点；未注册 = fail-closed 拒发，动作层不再接触 SMTP。
2. internal/service 新增 `marketing_flow_reach_gate.go`：注册「闸门版」短信/邮件发送器——判定链通过后才走原真实通道（`lazySendSms` / SMTP 传输原样搬移）。
3. 判定实例由 app 装配（与催收/挽回/报价各腿同构：构造 ProactiveReachService → AttachReachGate 装审批门 → AttachMarketingFlowReachGate 注入）；未装配 = fail-closed。
4. 流程撤回/暂停对在途执行也生效：executeFlow 把流程引用挂上 ctx，发送型动作发送前按流程 ID 复核状态（非 active 拒发；读态失败 fail-closed）。

**Files likely to change**:
- `user-server/internal/content/service/marketing_flow_send_gate.go`（新增：注入点/ctx 引用/状态栅栏/配置解析）
- `user-server/internal/content/service/marketing_flow_action.go`（改：两个发送动作接栅栏与注入点）
- `user-server/internal/content/service/marketing_flow.go`（改：executeFlow 挂流程引用）
- `user-server/internal/service/marketing_flow_reach_gate.go`（新增：闸门版发送器与判定链）
- `user-server/internal/service/marketing_flow_sms_init.go`（改：注册职责移走）
- `user-server/internal/app/marketing_flow_gate_wiring.go`（新增装配）
- `user-server/cmd/api/main.go`（改：router.Setup 后挂闸门）

## Tests to add or update

- internal/service：判定链矩阵——未装配 fail-closed、审批拒绝、DNC 命中、静默时段、日上限配额不足 ⇒ 真实传输零调用；放行路径恰好传输一次。
- content 层：暂停/停用流程的在途执行零外发且执行 failed 带可诊断原因；active 对照组照常发送；邮件未注册 fail-closed；注册后收到解析好的配置。

## Risks & Considerations

- 生产默认（旗子 off）下审批门不装但 DNC/合规仍生效——与 ReachByCustomer 显式路径的默认姿态一致；旗子开且 W-1 就绪后审批自动生效。
- 判定链拒绝会使动作节点失败（执行记 failed）——与同步 AI 工具路径「拒绝即报错」的既有语义一致，不在本项范围内引入自动退避。
- 内存构造的流程图（测试夹具，流程 ID=0）不触发状态复核，保持既有图测试语义不变。
- 邮件传输行为逐字保留原实现（MIME 构造、PlainAuth、SendMail 参数不变），仅位置随闸口走。
