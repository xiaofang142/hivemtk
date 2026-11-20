# Bug Verification: WhatsApp 线索交接

- Slug: whatsapp-lead-handoff
- Date: 2026-11-20
- Assessment: [assessment.md](assessment.md)
- Fix: [fix.md](fix.md)
- Verdict: PASS — 功能、交付和提交前仓库总门禁均已验证

## Regression

修复前 3 项实际失败：[red.log](evidence/red.log)。修复后真实 Vue Router、Pinia、Element Plus 与 fullPath 组件 key 覆盖跨页预选、取消、重置、错租户、号码改变后二次确认、删除联系人拒发、旧 URL/缓存不重放。工具用例覆盖作用域、过期、去重、跨页解析、无号码、非法响应与接口故障。连同既有 HTML 沙箱回归共 19 项通过：[focused-final.log](evidence/focused-final.log)。

全量 SPA 104 文件、1031 项通过：[full-vitest.log](evidence/full-vitest.log)。最终 eslint 退出 0，无警告输出；最终构建退出 0：[release-build.log](evidence/release-build.log)。

## Browser Evidence

ego-browser 使用同一 Space，逐个明确操作元素，每次操作后读取快照；夹具使用原始 SFC，接口只访问本机合成服务。

| 验收条件 | 真实证据 |
| --- | --- |
| 甲预选且展示当前姓名和号码 | [05-current-preselected.txt](evidence/browser-05-current-preselected.txt) |
| 发送前确认实际名单、模板和账号 | [06-confirmation.txt](evidence/browser-06-confirmation.txt) |
| 取消后对话框消失，未发送 | [08-cancel-settled.txt](evidence/browser-08-cancel-settled.txt) |
| 重置清空右侧选择与收件人预览 | [09-reset.txt](evidence/browser-09-reset.txt) |
| 重新只选乙，甲不串入 | [11-new-selection.txt](evidence/browser-11-new-selection.txt)、[12-new-preselection.txt](evidence/browser-12-new-preselection.txt) |
| 实际 reload 后旧标识无效且选择为空 | [13-reload.txt](evidence/browser-13-reload.txt) |
| 整轮发送请求为零，真实外发为零 | [browser-send-evidence.json](evidence/browser-send-evidence.json) |

## Production

[production.json](evidence/production.json) 保存核验时间、完整回滚包摘要和九份本地/服务器/公网资产一致结果。入口、两页面、交接模块及实际路由分块均 HTTP 200；index SHA-256 为 `995bdfe4352776dc4d891791f7b8a0691861186a5dd1da6403f44ca526a7e950`。未以静态资源核验替代浏览器交互证据。

## Repository Gates / Limits

`make audit`、`make lint`、`make vet` 均退出 0：[static-gates.json](evidence/static-gates.json)。`make test-go` 使用专用 PostgreSQL 8217 和任务独立 Go 缓存完整执行，退出 0，159 条包级通过记录、零 FAIL：[结果](evidence/go-isolated-gate.json)、[完整日志](evidence/make-test-go-isolated.log)。首次全量执行受共享 Go 缓存归档文件消失影响，未计通过；独立缓存重跑才是本项验收结果。

审计明确跳过未 checkout 的对端仓比对、缺 profile 的 Go 覆盖率、无实例的本机 8204 探针，这些维度不计通过。Graphify 缺模块未刷新；未执行真实 WhatsApp 外发。构建前缀是父提交，实际修复源码绑定 [tested-source-sha256.json](evidence/tested-source-sha256.json)。
