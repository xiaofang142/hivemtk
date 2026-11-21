# Bug Fix: FA-21 触达预检零副作用

- **Slug**: proactive-reach-dry-run-20261121
- **Fixed**: 2026-11-21
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

手机号和邮箱预检在退订检查后、发送审批/合规/发送与成功计数前返回 dry_run。客户预检复用正式 pickChannel 的真实账号读取，去除虚构账号；不改变现有预览不执行发送审批的契约。

## Changes

| File | Change | Notes |
|---|---|---|
| user-server/internal/service/proactive_reach.go | modified | 显式分支短路；复用只读选路；统一预览响应 |
| user-server/internal/service/proactive_reach_dry_run_test.go | added | 零外发/额度/冷却、正式闸口九格、真实账号选路 |
| user-server/internal/controller/proactive_reach_dry_run_test.go | added | validate 强制预检和 send 尊重 dry_run，六个 HTTP 场景 |

## Tests Added or Updated

- TestProactiveReachDryRun_NoSideEffects：phone/email/两者并存/customer；发送 spy 与 SMS registry 都零调用；审批零调用；额度不变；紧接的正式发送成功且才累计额度。
- TestProactiveReachDryRun_UsesActualAccount：telegram 活跃账号42与无账号；预览和正式选路一致，无账号均返回 ErrNoActiveAccount。
- TestProactiveReachDryRun_RealSendKeepsGates：三种目标 × DNC/审批/合规，九种拒发零外发。
- TestProactiveReachAPI_DryRunNeverSends：validate 即使 dry_run=false 仍强制预检；send 的 dry_run=true；三种目标的 HTTP data.status/message_id 均为 dry_run。

## Local Verification

- red.log：旧源码确定性复现三种显式请求外发并消耗额度；虚构账号及无账号假成功也复现。
- green-final.log：修后专项 service 1.839s、controller 1.358s，全部 PASS。
- gates.json：build 0（19.036s）、vet 0（23.512s）、层边界0（1626文件、0违规）、architecture 0、GORM-only 0、lint 0（0 issues）。
- 旧 check_naked_goroutine.sh rc=1：枚举存量含 recover/豁免的裸go站点；当前实际两道门 check-bare-go.py / check-naked-goroutine.py 均rc=0，81/81、0 hit，见 current-concurrency-gates.json。
- make audit rc=2：其他会话在途 docs/INDEX.md 指向未暂存 integration/PILOT-CONTRACT.md；未替其他会话暂存文件。
- make test-go-fast 已启动，最终结果由 test.md 记录。运行中已出现 bridge 能力存储准入失败。baseline-bridge.log 使用 baseline-overlay.json 将本轮唯一生产文件替换为 Git HEAD 原版编译，同一 TestSendManualMessage_AccountOwnershipDoesNotAuthorizeShop 仍失败（expected capability_mapping_missing，got capability_storage_unavailable），不是本修复引入。
- 未真实外发、未写生产、未部署、未提交/推送。

## Deviations from Assessment

无产品修复范围偏离。新增测试首次把“全局退订”夹具仅设为 sms，客户手机号还可作为 WhatsApp 身份，实际落入下一候选渠道而非全局退订；已将夹具改为 DoNotContactChannelAll 并复跑通过，未为测试改产品逻辑。

## Follow-ups

交付限制沿用唯一清单 FA-21 原行；本报告不是新增任务入口。完整门禁和发布状态由 test.md 及原清单写实，不从专项通过推定全仓/线上验收。
