# Bug Fix: SYS-01 后续静态门阻塞

- **Slug**: sys01-local-gates-20261120-2
- **Fixed**: 2026-11-20
- **Assessment**: [assessment.md](./assessment.md)
- **Status**: applied

## Changes

`scripts/bare-go.baseline` 仅删除 kb_crawler.go 已不存在的 2 个站点。全仓裸 go 实测 81，删除后登记也为 81；没有提高阈值或关闭门。

`user-server/internal/aiagent/knowledge/service/knowledge.go` 仅删除没有调用方的三行私有 persistChunkEmbeddings 方法；带 source 的正常写入和带归属栅栏的重建写入均保持原样。未改其他知识库行为。

## Local Verification

修复前实际失败：`make audit` 退出 2（STALE）；`make lint` 退出 2（unused）。修复后静态门及已有控制测试结果见 test.md。无新增业务行为，不为死代码删除构造镜像测试。

## Deviations from Assessment

无。原始 check_naked_goroutine.sh 的其他域存量报告保留，不为让该辅助脚本变绿改动无关代码，也不改其规则。

## Delivery Boundaries

不提交、不发布；无生产副作用；原历史报告不覆盖。当前完整 Go 回归只启动一次，已在运行；本轮后续源码修改仅移除不可达包装，不改变任何被测运行路径。
