# 上传边界验证

Result: partial（受控回归通过，尚未部署或进行真实上传）。

依据[assessment.md](assessment.md)和[fix.md](fix.md)。执行`sh_1e84b99d2001aAJj2vnlwSF8Le`：

```text
go test -p 1 ./internal/repository ./internal/service -run '^Test(ObsUploadConfig_|ObsUploadBoundary_|ObsConfig)' -count=1
repository: PASS 2.222s
service: PASS 1.515s
```

仅使用127.0.0.1:8217专用测试库及合成AK/SK：验证两租户签名隔离、策略解码与HMAC协议、20MiB/1小时、未知上下文拒绝、非法输入、HTTPS/裸地址/中文路径、active默认项选择、原管理端加密读写、未装配数据库、读取故障及读取后取消。未修改真实凭证、调用生产上传或清理用户数据。

静态lint此前0 issues；新增代码仍须纳入最终完整Go门禁。此结果不证明对象存储服务实际接受上传，不证明客户已收件。
