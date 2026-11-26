# Bug Verification: 渠道模块与消息归属

- **Slug**: `channel-module-semantics-20261126`
- **Tested**: 2026-11-26
- **Assessment**: [assessment.md](assessment.md)
- **Fix**: [fix.md](fix.md)
- **Result**: `partial`

## Summary

限定回归证明：导航按原授权收敛为一个渠道入口；同一账号按消息识别单聊与群聊；SageX 历史绑定保留 QQ／个人微信来源。四川同咨关联数据已在线只读核实，但新代码尚未发布，不能把现有生产页面当作修改后的实机验收。全量测试和审计仍有失败，未以修改其他在途工作或审计基线的方式规避。

## Checks Performed

| Check | Command / Action | Result | Notes |
| --- | --- | --- | --- |
| 生产数据基线 | ego-browser 使用已有登录态打开 `/channels/manage`，读取快照；只读核对渠道、账号及消息接口 | pass | 11 条渠道记录，含四川同咨／`sagex-t18`、QQ／`qq`；四川同咨客服账号仍关联原记录。没有重置识别码。 |
| 修改后实机验收 | 一个导航入口、授权页签、四川同咨绑定及消息归属 | not-run | 未发布；现有页面仍为旧代码。较早的空列表现象未在后续快照持续复现，不宣称解决了该独立问题。 |
| 本地 UI 逐项验收 | 隔离树 Vite 预览加载实际渠道 SFC，使用内存示例 API；逐项检查导航、页签、编辑、备注保存、权限投影及识别码页 | pass | 临时快照 `channels-ui-{channels,qq-edit,binding-edit,accounts,account-edit,account-saved,permission-tabs,identify-codes}.txt`。预览明确不连接生产；备注保存仅改内存 fixture，未执行识别码重置。 |
| 限定后端回归 | `go test ./internal/model ./internal/service ./internal/bridge/... -run 'Test(Channel|BindBridgeTenant|ResolveBridgeTenant|BridgeTenantForRequest|SystemMenu|Module)' -count=1` | pass | `internal/model`、`internal/service`、`internal/bridge` 均 `ok`；使用独立 Go 缓存及本地测试数据库，日志见 `channels-focused-go-final.log`。 |
| 限定前端回归 | `npm run test -- tests/unit/badCase_render.test.js tests/unit/browser_editor_dependency.test.js tests/unit/clue_list_filter_push.test.js tests/unit/channels_module.test.js tests/unit/menu_navigation.test.js --maxWorkers=1` | pass | 5 文件、45 用例通过；包含本轮渠道导航与此前全量回归暴露的 3 个超时文件，日志见 `channels-final-web-targeted.log`。 |
| 最后一处文案修改后组件回归 | `npm run test -- tests/unit/channel_semantics.test.js --maxWorkers=1` | pass | 4／4 通过。 |
| 前端静态检查 | `npx eslint src/views/channels/index.vue src/views/channels/channel-manage.vue src/views/channels/account-manage.vue src/views/channels/identify-code-manage.vue src/utils/menuNavigation.js src/constants/channel.js src/router/modules/channels.js` | pass | 无错误；此前完整 `npx eslint src --max-warnings 80` 也通过。 |
| 前端生产构建 | `npm run build` | pass | 最后一处文案修改后再次通过，生成 PWA 产物。 |
| 全量前端回归 | `npm run test -- --maxWorkers=2` | fail | 1293 个用例中 1276 通过、17 失败；139 文件中 136 通过、3 失败，失败集中在 `badCase_render.test.js`、`browser_editor_dependency.test.js`、`clue_list_filter_push.test.js`，详见下文。 |
| 修复前 Bridge 对照 | 使用 `go test -overlay` 将 `tenant_binding.go` 替换为 HEAD 版本，运行两项身份用例 | historical fail | 修复前识别码预期及查询错误传播用例失败；最终定向回归已覆盖并通过，旧日志仅作历史对照。 |
| 后端构建、vet、lint、分层 | 独立缓存、`GOFLAGS=-p=1`、`GOMAXPROCS=2`、`GOGC=30`，顺序执行 `go build ./...`、`go vet ./...`、`golangci-lint run ./... --timeout 20m`、分层检查 | pass | 四项均退出 0；golangci-lint 输出 `0 issues.`，日志见 `channels-final-static-gates.log`。 |
| 包级 Go 全量测试快照 | `go test ./internal/model ./internal/service ./internal/bridge/... -count=1` | fail | 较早快照中，SageX 测试假对象 panic，Bridge 包有 HTTP fixture 失败及当时的身份回归失败；后者已修复并由最终定向回归覆盖。详见 `channels-isolated-go-packages.log`，不作为最终代码全量通过的证据。 |
| 全量 Go 门 | `make test-go` | interrupted | 后台任务因 OpenCode 服务重启被取消，未取得整套退出码；不能将未完成包判为通过。最终渠道定向回归和 build/vet/lint/分层静态门通过。 |
| 规范审计 | `make audit` | fail | i18n 门统计 4 条孤立 locale 词条，相比基线 0 变差；完整审计在此门停止，后续门未执行。未修改审计基线。 |
| 五层依赖边界 | `python3 scripts/check-layer-boundary.py` | pass | 扫描 1785 个非测试 Go 文件，新增违规 0；本次静态门日志见 `channels-final-static-gates.log`。 |
| 差异格式 | `git diff --check` | pass | 无空白错误。 |
| 文档链接 | `python3 scripts/check-md-links-offline.py .` | pass | 扫描 842 个 Markdown 文件，断链 0；三份本轮报告已单独加入索引供检查，未提交。 |
| 视图质量检查 | Impeccable detect：四个渠道视图 | pass | 返回 `[]`。 |
| 知识图谱刷新 | `python3 -c 'from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path("."))'` | not-run | 当前 Python 环境缺少 `graphify` 模块；没有安装额外工具。 |

后端限定测试模式：

```text
Test(Channel|BindBridgeTenant|ResolveBridgeTenant|BridgeTenantForRequest|SystemMenu|Module)
```

## Output Excerpts

```text
ok  hivemtk-user/internal/model    0.475s
ok  hivemtk-user/internal/service  1.982s
ok  hivemtk-user/internal/bridge   0.952s
Test Files  5 passed (5)
Tests       45 passed (45)
```

全量前端剩余失败：`badCase_render.test.js` 11 项、`browser_editor_dependency.test.js` 1 项、`clue_list_filter_push.test.js` 5 项，均表现为既有测试超时/连带断言失败。这些用例没有被删除、跳过或调高超时；定向串行重跑 3 个文件已通过，未修改这些非本任务文件。

包级测试快照的 Bridge HTTP fixture 失败包括 `TestBridgeHTTP_OutboundEcho_SkippedByContentHash`、`TestBridgeHTTP_ImmediateReturn_NoLongPoll`、`TestBridgeHTTP_MultiChannel`、`TestBridgeHTTP_Concurrent`、`TestBridgeOutboxAck_RoundTrip` 和 `TestBridgeOutboxAck_ScopeIsolation`。同一快照里若干身份测试也失败，但最终渠道定向回归均已通过。

全量 Go 包回归此前因无关 SageX 测试假对象触发 panic，并伴随若干旧 Bridge HTTP fixture 失败；本轮渠道定向模式已重新通过。构建、vet、lint、分层检查在修复后新鲜运行均退出 0；不将限定回归结果外推为全量 Go 通过。

完整日志保存在本次会话临时目录 `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/`：`channels-focused-go-final.log`、`channels-final-web-targeted.log`、`channels-verification-web-{component,eslint,build}.log`、`channels-isolated-web-all.log`、`channels-final-static-gates.log`、`channels-final-audit.log`、`channels-baseline-lvuptqtw/baseline-tests.log` 和 `channels-isolated-go-packages.log`。报告不记录凭据。

## Residual Risks

- 原三个 URL、权限键和账号关联通过限定回归检查；发布后的真实角色导航与交互仍需逐元素验证。
- 生产历史消息中的服务编码未迁移；本次修正后续消息的解析语义，不追溯改写数据。
- 旧 Type 字段仅作兼容保留；未改变数据库结构，也未批量更新租户渠道配置。
- 工作区存在其他会话的在途修改，全量门禁反映整个工作树，不能等同于仅本次修改的干净检出验证。

## Recommendation

保留 `partial`，暂不关闭为线上已修复。先解决或归属全局门禁失败、核清发布范围，再按用户授权发布并使用 ego-browser 对真实页面和消息逐元素验收。本轮未提交、推送、发布或写入生产配置。
