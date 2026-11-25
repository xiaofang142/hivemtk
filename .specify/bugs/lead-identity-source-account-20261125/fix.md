# Bug Fix: 线索自动入库身份与来源账号语义

- **Slug**: lead-identity-source-account-20261125
- **Task**: LM-20261125-04

## Changes

- `service/lead_mining.go`：新增 `leadMiningUnifiedIDFor`，有专用身份列时用 OneID 归一，无专用列时用 `lm:<platform>:<sourceAccount>:<sender>` 确定性作用域键，杜绝随机 UUID。
- `resolveCustomer` 增加 `sourceAccountID` 参数，`Customer.SourceAccountID` 写真实接入账号而非联系人 ID。
- `service/lead_group_identity.go`：单聊调用点传入 `hub.AccountID`；群聊身份逻辑不变。

## Boundary

个微/QQ/Telegram 等专用身份分支不变；不改 OneID 主逻辑；无 UI/外发/发布/提交。
