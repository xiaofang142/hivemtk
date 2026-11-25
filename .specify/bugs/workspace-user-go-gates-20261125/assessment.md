# Bug Assessment: 工作区提交前 Go 检查失败

- **Slug**: workspace-user-go-gates-20261125
- **Created**: 2026-11-25
- **Source**: 工作区整体提交前执行用户端 Go build、测试、audit 与 lint
- **Verdict**: valid
- **Severity**: medium

## Symptom and Reproduction

原始完整输出位于工作区 `.planning/2026-11-25-workspace-commit/user-go-{tests,audit,lint}.log`。
测试发现新增 GORM 模型表未登记模块归属；audit 的 stdout gate 发现 edition 包绕过结构化日志；lint 报告资源关闭/缓存写错误未检查、无效赋值、静态检查与格式问题。

## Root Cause

近期新增模型没有同步登记至模块表归属表；少数持久化/文件操作错误未向调用方返回或记录；旅程 CAS 对读故障未做 fail-closed 处理；部分检查器反馈的是有效逻辑/代码格式问题。

## Proposed Remediation

按现有模块所有权登记新表；保留并传播关键读写和关闭错误；修复静态检查指出的无效赋值及标准库日志旁路；只对 lint 指定文件运行 gofmt，不放宽任何检查配置。

## Tests to add or update

使用已有模型归属测试、Go vet、服务层定向测试及工作区全量门禁验证。

## Risks and Considerations

跟进记录归属 sales-crm；SageX 接收回执、Webhook 投递及渠道接入表归 core；客户画像/导入/授权/线索表归 customer；SOP 定时器认领表归 ai-sales。后续测试仍需确认当前实现通过。

## Final Integration Findings

完整 `user-go-final-tests.log` 与定向复现 `user-go-integration-repro-{wire,crm,outbound}.log` 还发现以下集成差异：

- 心跳请求已增加可选 `version_type`，用户端反射协议清单没有重新生成。
- CRM 草稿内存仓储替身只给副本赋创建时间，返回对象仍是零时间，首次确认因此命中版本冲突；正式确认逻辑还缺少已应用草稿的幂等返回，审计失败时未返回已经发生的业务结果。
- 显式桥接账号现要求真实数值主键、正确租户与 edition；旧出站夹具使用任意字符串账号，部分新数值夹具没有显式 custom edition，因此在测试目标分支之前已被拒绝。
- lint 最后两项是发送凭据字段未 gofmt，以及旅程 CAS 的空 else 分支。

修复应重新生成协议产物并校验平台兼容性；使草稿替身遵循真实仓储的时间回填行为，恢复顺序重复确认与审计错误的可见结果；逐项迁移成功路径夹具到真实账号和显式版本上下文，非法账号用例验证拒绝且零外发。保留生产账号、租户、edition 与授权门禁，不放宽它们来适配旧夹具。以上定向复现均已返回非零；修正后先复验相关用例，再运行完整 Go 门禁。
