# Bug Verification: FA-EMAIL-02/03

- **Slug**: email-unsub-failure-and-smtp-decrypt-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地全绿）

## 关闭判据对照

- FA-EMAIL-02「查询故障独立于未退订，拒发或可恢复延期，保留故障原因；假退订仓储对即时、排期分别注入故障，假发送器计数必须为零」✔（即时：错误返回+零投递；排期：deliverClaimed 零投递+行不标 failed 可续投；两腿独立注入）
- FA-EMAIL-02「恢复后按实际名单判定」✔（行保持 pending，认领重查路径就是恢复后判定路径）
- FA-EMAIL-03「所有选路复用统一、拒绝错误密钥的凭据解析，不把密文当密码或悄悄切换其他账号」✔（smtpPassword 唯一出口；显式分支解密失败报错不切换；默认候选行解密失败跳过）
- 「用合成加密密码…覆盖指定/默认/备用及解密失败，零真实拨号」✔（加密往返、非法密文、备用行种子全部走加密；全程无真实 SMTP）

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 专项矩阵 | go test ./internal/email/service -run 'TestEmailUnsubscribe.*\|TestEmailUnsubscribed.*\|TestExplicitSmtp.*\|TestResolveSmtpFromDB' -count=1 -v | pass | green.log 9 PASS |
| 专项 race | 同上 -race | pass | race.log |
| email 全包回归 | go test ./internal/email/... -count=1 | pass | regression.log 全 ok |

## 边界

- 与 FA-15 统一联系授权入口的整合仍按原行跟踪（本项只修退读故障与凭据解析）。
- 与 FA-EMAIL-01 的即时/排期唯一认领独立，未在本项处理。
- 未运行真实 SMTP/真实收件人；未提交/推送/部署。

## Evidence Files

fix.md / green.log / race.log / regression.log / source-sha256.json
