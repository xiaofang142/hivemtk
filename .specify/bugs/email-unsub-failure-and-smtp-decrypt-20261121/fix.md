# Bug Fix: 退订名单查询故障拒发 + SMTP 凭据统一解密（FA-EMAIL-02/03）

- **Slug**: email-unsub-failure-and-smtp-decrypt-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

FA-EMAIL-02：isUnsubscribed 把 ExistsByEmail 的所有错误返回 false ⇒ 退订库故障期间照常投递（即时 185 与排期 291 两条腿同病）。FA-EMAIL-03：显式 SmtpID 路径把仓储原样读出的密文直接交给 gomail.NewDialer（同一账号显式选择与默认选择行为相反）；默认路径 smtpRowReady 解密失败时把密文留在字段里照常返回。

修复：
- isUnsubscribed 返回 (bool, error)，故障与"未退订"分离；即时腿查询故障 → 返回错误拒发（行保持 pending，认领时重查=可恢复延期）；排期腿查询故障 → 本轮跳过待续投，不标 failed；命中退订照旧标 failed/跳过。
- 新增统一凭据出口 smtpPassword（空密码/解密失败/解密空值都明确报错）；显式 SmtpID 分支解密后使用，解密失败 fail-closed 报错、绝不悄悄换账号；smtpRowReady 解密失败拒绝该候选行（默认通道换下一行的既有选取语义保持）。

## Changes

| File | Change |
|---|---|
| `internal/email/service/email_send.go` | isUnsubscribed 返回错误；即时/排期两腿故障处理；smtpPassword 统一出口；显式分支解密 fail-closed；smtpRowReady 拒绝解密失败行 |
| `internal/email/service/email_compliance_credentials_test.go` | 新增 5 用例（即时/排期故障拒发零投递、退订命中零投递、统一出口解密正确、非法密文 fail-closed） |
| `internal/email/service/email_smtp_priority_test.go` | 既有种子行明文密码改加密种（新语义：明文行=不可用凭据） |

## Local Verification

- 专项 5+4 用例 + `-race` 通过（green.log / race.log）。
- `go test ./internal/email/...` 全包 ok（regression.log，快测实例凭据由容器环境读入）。
- 全仓 build 通过。
