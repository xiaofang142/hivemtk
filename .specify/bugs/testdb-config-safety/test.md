# Test Report: AV-01/02 测试库配置保护

- **Slug**: testdb-config-safety
- **Tested**: 2026-11-10（清单整理轮行政收口——实质修复由 A-1/F-4-1 轮落地，本报告补录验证口径）
- **Assessment**: [assessment.md](assessment.md)
- **Verdict**: verified

## 验证证据（逐条对应 assessment「Tests」要求）

1. **显式端口不回退**：A-1（2026-11-08）落地端点钉扎——candidates 改钉扎版（显式端口唯一时只用它；否则 8217+声明端口组合），**业务端口 8202/8232 不再参与探测**；F-1 复检带环境实跑全绿（testutil 全包+端点/指纹/maskedDSN 用例）。
2. **缺密码报配置错误（fail-closed）**：`NewTestDB` 前置 `readTestDBConfig` 安全门，连网前 fail-closed，错误原文透传；F-1 负例矩阵 PASS（缺口令 fail-closed 原文透传）。F-4-1（2026-11-09）修正 DSN 非空 + URL 内嵌口令与前置门的优先级冲突（DSN 显式覆盖一切），补「URL 内嵌口令 + 空环境」正例钉死。
3. **日志脱敏**：`maskedDSN` 三形态全覆盖（多 password= / 引号口令 / URL userinfo）；基名白名单（仅 `user_db_test`/`platform_db_test`）+ 重复 dbname/注入形态拒绝由 testdb_config_test.go 用例覆盖。
4. **Makefile 入口凭证门**：`TESTDB_AUTH_GATE` 门禁宏在位并被 `make test-go`（Makefile:505-507）消费；A-13 增 `test-env` 目标辅助导出口令。
5. **回归**：F-5 e2e（2026-11-09）+ K-4-3 全量 `go test ./...`（2026-11-10，PG 8217 + EMBEDDING_ALLOW_FALLBACK=true）绿；两端 build/vet 终局门 0/0。

## 结论

assessment 所列风险（显式测试端口参与多端口探测回落业务库、口令缺失回落硬编码、日志泄露凭证、masker 覆盖不全）均已由 A-1/F-4-1 修复并多轮回归验证；lane 关闭。
