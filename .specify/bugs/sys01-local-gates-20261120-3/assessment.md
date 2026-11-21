# Bug Assessment: 裸协程门自测的固定读数过期

- **Slug**: sys01-local-gates-20261120-3
- **Created**: 2026-11-20
- **Verdict**: valid
- **Severity**: low
- **Source**: `bash scripts/check-bare-go.test.sh` 真实退出 1

## Root Cause and Reproduction

前 7 组正反控制通过；B15 在真实门已退出 0、现算 81 / 基线 81 时仍要求历史固定值 83/83。FA-12 同步处理已实际移除 2 个站点，上一轮已按棘轮要求删除失效基线。自测的固定读数需要同步降低，而非放宽规则。

证据：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/sys01-closure/bare-go-controls.log`。

## Proposed Remediation

**Files likely to change**: `scripts/check-bare-go.test.sh`，仅把 B15 的固定期望及成功提示从 83/83 更新为独立门实测的 81/81。

保留 B1–B14 全部违规/保护/格式控制及 B15 的门退出码与精确总数断言；不删除断言，不增加可接受范围，不改扫描器。

## Verification

重跑同一脚本，要求全部 8 组通过；运行 shellcheck error 与 bash 3.2 解析。此项仅维护与已审查基线一致的固定测试数据，无运行时行为改变。
