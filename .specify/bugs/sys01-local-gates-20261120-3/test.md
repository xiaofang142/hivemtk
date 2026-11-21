# Bug Test: 裸协程门自测固定读数

- **Slug**: sys01-local-gates-20261120-3
- **Date**: 2026-11-20
- **Assessment**: [assessment.md](./assessment.md)
- **Fix**: [fix.md](./fix.md)
- **Result**: PASS

日志目录：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/sys01-closure/`。

| Command | Exit | Evidence |
|---|---:|---|
| 修复前 `bash scripts/check-bare-go.test.sh` | 1 | bare-go-controls.log；B15 固定 83/83 与实测 81/81 不符 |
| 修复后同一命令 | 0 | bare-go-controls-green.log；PASS[1]–PASS[8]、ALL PASS |
| `shellcheck --severity=error scripts/check-bare-go.test.sh` | 0 | shellcheck-changed.log |
| `/bin/bash -n scripts/check-bare-go.test.sh` | 0 | bash32-changed.log；系统 bash 3.2 |

修改仅两行：精确期望及成功提示降为 81/81。缺基线、NEW、STALE、OVER、格式错误和保护形态全部控制保留，门本身未修改。完整 Go/audit/lint 结果、最终源码哈希与限制见 [主验证报告](../sys01-local-gates-20261120/test.md)。
