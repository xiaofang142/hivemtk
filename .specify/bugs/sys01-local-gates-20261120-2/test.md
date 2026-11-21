# Bug Test: SYS-01 后续静态门阻塞

- **Slug**: sys01-local-gates-20261120-2
- **Date**: 2026-11-20
- **Assessment**: [assessment.md](./assessment.md)
- **Fix**: [fix.md](./fix.md)
- **Result**: PASS（静态阻塞修复）；整体限制见 [主验证报告](../sys01-local-gates-20261120/test.md)

日志目录：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/sys01-closure/`。

| Command | Before | After | Evidence |
|---|---|---|---|
| `make lint` | 退出 2，unused persistChunkEmbeddings | 退出 0，0 issues | lint.log、lint-2.log |
| `make audit` | 退出 2，crawler 裸协程基线 STALE | 退出 0，静态审计通过 | audit.log、audit-2.log |
| `python3 scripts/check-bare-go.py` | STALE，登记 83 / 实际 81 | 退出 0，81/81 | audit.log、bare-go-green.log |
| `go test ./internal/aiagent/knowledge/service -count=1 -timeout 10m -v` | — | 退出 0，94 PASS / 5 环境 SKIP / 0 FAIL | final-knowledge.log |
| `go build ./...`（user-server） | — | 退出 0 | final-build.log |
| `make vet` | — | 退出 0 | final-vet.log |

已有控制脚本最初退出 1：前 7 组通过，第 8 组固定 83/83 过期；单独评估并在 [第三轮](../sys01-local-gates-20261120-3/test.md) 修复，重跑 8 组退出 0。没有提高基线、关闭检查或保留僵尸条目。

audit 本地实例探针 SKIP，不代表在线 API 已验证。原始 user-server/scripts/check_naked_goroutine.sh 仍报告范围外存量，graphify 模块缺失。源码哈希及环境恢复证据见主报告；未提交或发布。
