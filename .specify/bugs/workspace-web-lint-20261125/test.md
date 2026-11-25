# Bug Test: 工作区提交前端 lint 失败

- **Slug**: workspace-web-lint-20261125
- **Fix**: [fix.md](fix.md)

## Verified

- `npm run lint:check -- --max-warnings=80` 退出码 0。
- `npm run build` 退出码 0。
- 自动排版前后 11 个文件 script 区块一致，cause 修正另行保留。

## Full Tests

双 worker 全量复验出现 3 个测试文件的超时及后续状态断言失败；这 3 个文件用单 worker 复跑，49 tests 全部通过。
完整单 worker 套件 `npm run test -- --maxWorkers=1` 退出码 0，136 个文件、1284 个测试全部通过。
