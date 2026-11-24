# Bug Verification: 客户导入作业权威状态写被吞

- Tested: 2026-11-25
- Assessment: ./assessment.md
- Fix: ./fix.md
- Result: verified（针对本缺陷）

## Checks Performed

- 新增 4 个定向单测覆盖 running 状态写失败、读文件失败、终态写失败与成功路径；`go test ./internal/service/... -run 'CustomerImportJob|ImportJob' -count=1` 全 ok。
- 状态写失败分支均有显式错误返回或 `logger.Errorf` 日志，不再静默。
- `python3 -m py_compile user-server/scripts/simulate/simulate.py user-server/tests/e2e/e2e_full_test.py` 通过；URL/路径防护为纯输入校验，未改动网络契约。

## Residual Risks

- 完整 `go test ./internal/service/...` 仍有 11 个既存失败，根因是本地测试库 schema 未迁移完整（缺 `channel_route_rules`、`feedback_records`）与 `CONFIG_PATH` 未指向仓库配置；这些用例与导入作业状态机无关，本次未声称其通过。
- `UpdateStatus` 未做 CAS（pending→running），并发重入边界仍属后续改造范围，见 assessment.md 的 Alternatives。
