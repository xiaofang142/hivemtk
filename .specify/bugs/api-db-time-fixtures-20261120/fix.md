# Bug Fix: PostgreSQL时间夹具精度

- Fixed: 2026-11-20
- Status: applied
- Assessment: ./assessment.md

仅将两个时间夹具在写库前截断到微秒：延迟发送future及会话活动t0。保留原有精确Equal断言、未来值不受影响与活动时间推进的检查；不改产码、不放宽断言。

完整Go测试中其余157个有测试包通过，仅repository的这两条时间精度断言失败。修复后完整repository包重跑通过：`go test ./internal/repository -count=1 -timeout 1800s`，194.965s。

证据见本轮checks目录的全量测试记录与repository-final记录。
