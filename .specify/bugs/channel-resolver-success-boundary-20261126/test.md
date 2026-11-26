# Bug Verification: 识别码命中后的渠道与租户边界

- Tested: 2026-11-26T07:31:16+08:00
- Result: partial；生产解析函数的 fake-only 回归通过，真实仓储及各渠道接线尚未验收。
- Assessment: ./assessment.md
- Fix: ./fix.md
- 关联：CH-20261125-07、LEAD-20261125-03；保留原任务的开放状态。

## RED

在 `hivemtk-user/user-server/` 执行：

```sh
go test ./internal/service -run '^TestChannelResolver_(IdentifySuccessEnforcesBoundary|TrustedAndWebhookDoNotIgnoreChannelFailure)$' -count=1
```

07:23:48+08 原生产代码 exit 1：识别码命中后接受错误平台、错误/缺失租户，以及不可用、查询失败、缺失或 ID/租户不符的渠道行；可信入口及 Webhook 还会吞掉渠道故障，后者返回已绑定 tenant=1。正向 Code/Type 匹配用例通过，失败来自缺失的边界校验。

07:27:07+08 附加满页用例单独复现：50 行中仅一个可见精确匹配被返回为唯一账号；完整查询尾部未知，因此不能据此确认唯一性。调整满页判断后该用例通过。

## GREEN 与兼容性

最终命令：

```sh
go test -race ./internal/service -run '^(TestChannelResolver_.*|TestModuleGateDecision_.*|TestAutoReplyEnabledForAccount.*)$' -count=1 -v
```

exit 0，`PASS` / `ok hivemtk-user/internal/service 2.053s`。19 个顶层测试（13 个 Resolver、4 个模块闸门、2 个数字账号闸门）及其子用例全部通过，无 race 报告。

新增六个顶层用例覆盖：识别码成功路径的 Code/Type 与租户约束、渠道仓储失败、已命中但不完整的主数据、最终复读账号不一致、满页不能证明唯一，以及健康 legacy miss 与未绑定租户的可信引导正例。保留旧用例的歧义拒绝、故障分类、租户隔离、渠道不匹配、数字账号和双模块绑定契约。

## 其他验证与边界

- `go build ./...`、`go vet ./...` 均 exit 0。
- 分层检查扫描 1785 个非测试 Go 文件，跨层违规 0；架构十组检查及 GORM-only 检查通过，架构仅两条既有命名警告。
- 裸 goroutine 检查仍有 62 处匹配；与 HEAD 的路径/文本多重集合完全相同，新增 0，未修改无关基线文件。
- Graphify 刷新因缺少 Python `graphify` 模块失败；未生成图谱证据。
- 用例仅依赖 fake 仓储、nil-DB seam 和生产判定函数；没有运行会调用外部模型的全量 service 套件，没有数据库、API、渠道、设备或生产验收。未提交、推送、部署，未修改并行脚本和旧 bug 报告。
