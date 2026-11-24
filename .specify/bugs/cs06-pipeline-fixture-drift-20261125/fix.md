# Fix: CS06 检查点夹具迁移

- **Slug**: cs06-pipeline-fixture-drift-20261125
- **Status**: applied（test-only）
- **File**: `user-server/internal/channelgw/cs06_pipeline_checkpoint_test.go`

## Applied Changes

1. Canonical 主数据
   - 新增 `cg06SeedCanonicalAccounts`，播种租户内 `Channel` 与启用 `ChannelAccount`；
   - `cs06-acc-*` 外部字符串改为 canonical 数值账号 ID `1` / `2`；
   - 跨租户用例为 A 播种 canonical 渠道/账号，B 账号复用全局唯一渠道主数据但归租户 B，只验入站隔离与认领不可见。

2. 测试 setup
   - `FeedbackRecordORM` 加入 AutoMigrate 模型清单，消除 `feedback_records` 缺表；
   - `Channel` / `ChannelAccount` 加入模型清单；
   - 国内桥接渠道测试显式设置 `edition.TypeCustom`，cleanup 恢复原版本。

3. E3 负例迁移
   - `-unknown` 显式账号不再等待 failed outbox；
   - 改为等待真实 `ai_result` 安全出口后断言该会话 **0 条 outbox**，保持错误可见且零外发。

4. 确定性异步收尾
   - 新增 `cg06AIDrain` 包装真实 `AITrigger`，记录被触发会话；
   - 通过生产 `InboxAIProcessingKey` 处理标记释放等待 `runAIGeneration` defer 完成；
   - 测试结束与 cleanup 均等待 AI 收尾后再关闭 DB；未放大 10s 等待上限。

## Boundary

- 仅测试文件 unstaged 修改；staged blob `8254d9138b6f1b1b81793bc94bb15795e6da6bef594837ac9cba96906b9b251d` 未变；
- 未改生产、未放宽 capability、未 commit/push/deploy/外发/UI。
