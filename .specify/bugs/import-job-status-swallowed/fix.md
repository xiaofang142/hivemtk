# Bug Fix: 客户导入作业权威状态写被吞

- Slug: import-job-status-swallowed
- Fixed: 2026-11-25
- Assessment: ./assessment.md
- Status: applied

## Changes

`user-server/internal/service/customer_import_job.go` 的 `Execute` 不再吞掉任何权威状态写错误：

1. 抢占 `running` 失败立刻返回包装错误，不进入 CSV 处理，避免作业停在旧状态导致既无结果又无法重试。
2. 读文件失败 / 空 CSV 后的 `failed` 状态写失败用 `logger.Errorf` 记录「状态写失败 + 原始错误」双凭据，原始错误仍照常返回。
3. 最终状态写失败返回 `report` + 包装错误，controller 不会再据此回成功假绿。
4. 新增 `readCSVFileFn` 注入点（nil 时走生产实现），使读文件失败路径可被单测覆盖。

`customer_import_job_test.go` 新增 4 个用例：running 写失败中止且不读文件、读文件失败保留根错误、终态写失败返回错误与 report、成功路径。

`user-server/scripts/simulate/simulate.py` 与 `user-server/tests/e2e/e2e_full_test.py` 同批加固：进度文件路径防目录穿越、目标 URL 限绝对 http(s) 且禁 userinfo/fragment/重定向（`allow_redirects=False`）。

## Verification

- `gofmt -l` 对两个改动 Go 文件输出为空。
- `go test ./internal/service/... -run 'CustomerImportJob|ImportJob' -count=1`：全部 ok。
- `python3 -m py_compile` 对两个改动 Python 脚本：通过。
- 完整 service 套件另有 11 个既存环境性失败（测试库缺 `channel_route_rules` / `feedback_records` 等表、CONFIG_PATH 未指向仓库配置），与本修复无关，未视为本缺陷回归。

## Deviations

无。未修改既有错误契约以掩盖失败；终态写失败仍把 report 交给调用方，同时显式返回错误。
