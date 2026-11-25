# Test: capability 夹具迁移

- 已验证：`TestBridgeSubmit_CapabilityDenialsHaveNoOutboxOrRetry` 迁移后 GREEN（rc=0，约 3.6s）。
- 其余迁移组因测试库并发竞争与进程卡滞，尚未取得稳定全绿读数；**不宣称全部 84 个失败已修复**。
- 待子 agent 测试活动结束后串行重跑全量确认。
