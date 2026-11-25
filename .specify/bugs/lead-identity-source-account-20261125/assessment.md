# Bug Assessment: 自动入库使用可信稳定身份并纠正来源账号语义

- **Slug**: lead-identity-source-account-20261125
- **Task**: LM-20261125-04
- **Verdict**: valid

## Report

`resolveCustomer` 把 `Customer.SourceAccountID` 写成 `senderID`（联系人身份），而不是真实接入账号；`applyLeadMiningIdentity` 对无专用字段渠道（sms/web/dingtalk 等）回退 legacy `lm:` key，`GenerateCustomerUnifiedID` 对未知渠道仍可能生成随机 UUID，导致同一联系人重复命中不能稳定归一。

## Scope

- `resolveCustomer` 接收真实来源账号（hub.AccountID / 规范账号），`SourceAccountID` 写入该值。
- 未知渠道使用带作用域（tenant/platform/account）的稳定 OneID，不靠随机 UUID。
- 个微/QQ/Telegram 等已有专用身份分支保持不变。
