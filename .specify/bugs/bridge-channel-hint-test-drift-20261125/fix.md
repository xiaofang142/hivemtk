# Fix: bridge channel hint 负例迁移

- **Slug**: bridge-channel-hint-test-drift-20261125
- **Status**: applied（test-only）
- **File**: `user-server/internal/bridge/tenant_binding_test.go`

将 `TestBindBridgeTenant_ChannelMismatchRejected` 迁移为 `TestBindBridgeTenant_ChannelHintDoesNotOverrideIdentifyCode`：

- 合法 identify_code + 错误 channel hint + 正确 account 不再期望 ambiguous；
- 断言统一身份采用识别码解析出的 canonical `douyin` 与账号；
- 断言 on 模式绑定识别码租户；
- account mismatch 负例原样保留。

未修改 `tenant_binding.go` 或任何 handler 生产代码；staged blob 保持 `f06911eb...` 对应 HEAD 版本不变。
