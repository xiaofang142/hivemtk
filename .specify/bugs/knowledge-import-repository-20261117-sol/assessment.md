# 限定接管知识导入凭证仓储边界

Verdict: valid / high。

## 证据及决策

限定接管的知识导入service直接引用根repository，被aiagent-layer门拒绝。保留同一tenant/key存储及AES-GCM读写、缺配置空结构、开发未初始化加密的兼容语义；新同域专用仓储负责SQL和上下文边界，不让service接触数据库。不改真实凭证、外部导入请求或API权限。另补Save(nil)错误返回，不能panic；新读写入口拒nil、System、取消及SaaS缺租户，不借默认租户放行。

## Files likely to change

- `user-server/internal/aiagent/knowledge/service/knowledge_import_credentials.go`
- 新同域`user-server/internal/aiagent/knowledge/repository/import_credentials.go`及测试
- 新`user-server/internal/aiagent/knowledge/service/knowledge_import_credentials_test.go`
- 已授权`user-server/internal/aiagent/knowledge/controller/knowledge_merchant.go`、`internal/service/webhook_dedup.go`、`webhook_recovery.go`、`internal/email/service/email_smtp_priority_test.go`：仅gofmt及EOF空白
- 本目录fix/test及主进度

## 验证

先验证架构直接依赖、nil配置与非法上下文的失败用例；正常tenant18/19读写、密文落库/透明读回与原对象不被修改是正向对照。仅专用PG127.0.0.1:8217，不访问外部API或生产库。
