# Bug Verification: Bridge 入站消费者越过消息上限

- Tested: 2026-11-26
- Result: partial；本机真实处理入口的原始症状已消除，真实渠道、数据库及生产验收未执行。
- Assessment: ./assessment.md
- Fix: ./fix.md
- 关联：CH-20261125-09/10、LM-20261125-06；不据此关闭整项任务。

## RED → GREEN

在 `hivemtk-user/user-server/` 运行：

```sh
go test ./internal/bridge -run '^TestHTTPIngestLimitAppliesBeforeAllConsumers$' -count=1
go test -race ./internal/bridge -run '^TestHTTPIngestLimitAppliesBeforeAllConsumers$' -count=1
```

修改生产代码前，真实 Gin handler 的投递器收到 201 条，预期为 200 条，并收到本应截断的 `live-200`，命令 exit 1。日志保留在 `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/bridge-limit-red.log`。

调整调用顺序后同一用例 exit 0，`ok hivemtk-user/internal/bridge 0.621s`；最终 `-race` exit 0，`ok hivemtk-user/internal/bridge 1.863s`，无 race 报告。

- 合法 201 条消息：投递器、历史持久化入口、实时处理入口各收到 200 条；响应也只有 200 条结果，截断项不出现在任何消费者。
- 最后一条消息含账号冲突：整批返回 400，三个消费者均零调用；未因先截断而绕过整批 scope 校验。
- 使用真实 HTTP 处理函数与 `httptest`，消费者为 fake；配置及账号全局仓储保存后替换并恢复，无数据库、Redis、真实 HTTP、模型或渠道请求。

## 其他验证

`go build ./...`、`go vet ./...`、`python3 scripts/check-layer-boundary.py`、`bash scripts/check-architecture.sh`、`bash user-server/scripts/check-gorm-only.sh` 均通过（后面三条在产品仓执行）。分层扫描 1785 个非测试 Go 文件，新增违规 0；架构脚本仅有两条既有命名警告。

`bash user-server/scripts/check_naked_goroutine.sh` 未通过。按脚本的匹配与排除口径比较全仓 HEAD/工作树的「文件路径 + 匹配文本」多重集合，两者均 62 处，新增与移除均为空；本次未新增 goroutine，不把该基线失败写为全仓通过。

按规则尝试 Graphify 刷新，因 `ModuleNotFoundError: No module named 'graphify'` 未完成。未提交、推送或部署；本次证据不证明真实归档、AI 执行或渠道端到端验收。
