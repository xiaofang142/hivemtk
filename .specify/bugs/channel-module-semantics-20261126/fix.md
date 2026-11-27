# 渠道模块与消息归属修复

- Slug: `channel-module-semantics-20261126`
- Fixed: 2026-11-26
- Assessment: [assessment.md](assessment.md)
- Status: `applied`（工作树代码已修改；尚未提交、推送或发布）

## Summary

渠道收敛为一个顶级导航入口，原有三个 URL 在模块内以授权页签切换。QQ、个人微信属于渠道，SageX 属于接入服务；单聊与群聊由消息决定。示例公司的历史接入绑定及其账号、识别码关联保留。

## Changes

| 文件 | 修改及目的 |
| --- | --- |
| `user-server/internal/model/{menu.go,module_bindings.go,menu_wiring_test.go}` | 新增独立渠道模块，保留原叶子权限键、路径和模块归属。 |
| `user-web/src/utils/menuNavigation.js`、`src/router/modules/channels.js`、`src/views/channels/index.vue` | 将已授权的渠道入口投影为一个导航项；三个旧路径共用容器，仅展示原有授权页签，保留深链。 |
| `user-web/src/views/channels/{channel-manage.vue,account-manage.vue,identify-code-manage.vue}` | 删除渠道及账号上的固定会话类型配置；历史 SageX 记录单列为接入绑定；补充服务说明、备注回填和绑定名称。 |
| `user-web/src/constants/channel.js`、`user-server/internal/service/channel.go` | 补齐 QQ、个人微信选项与预设，说明 SageX 接入关系。 |
| `user-server/internal/service/{channel_resolver.go,channel_identity.go,channel_inbound_orchestrator.go}` | 删除账号类型兜底和缺失类型默认单聊；编排器按消息 `IsGroup` 传递类型，避免把 QQ 等平台名当会话类型。 |
| `user-server/internal/bridge/{tenant_binding.go,tenant_binding_test.go}` | 对 `sagex`／当前租户的 `sagex-t<tenantID>` 历史绑定，仅接受 QQ、个人微信作为消息平台；普通渠道的凭据平台约束及租户、账号校验保持有效。查询故障不再被吞成未归属，跨租户歧义保持拒绝。 |
| `user-server/internal/service/channel_resolver.go` | 渠道读取使用租户受限上下文，同时保留请求的 edition 覆盖，避免 `OnlyTenant` 剥离版本值后把定制版国内渠道误判为不存在。 |
| `user-server/internal/dto/channel.go`、`internal/service/{channel_master.go,channel_account.go}` | 返回渠道备注；更新时未传 Type 不覆盖历史值，兼容旧客户端。 |
| `user-web/src/i18n/locales/*.json`、`src/config/onboarding/pageTours.ts` | 补充九种语言的模块和页面引导文案；导览使用既有 `setup.onboarding.page.*` 命名空间。 |
| `router-list.md`、`user-web/router-list.md` | 同步导航呈现变化，原路由台账与历史验收记录保留。 |

以上 `src/` 为前一文件所处的 `user-web/` 下路径；后端 `internal/` 路径位于 `user-server/`。没有数据库结构变更、生产配置写入或历史消息批量改写。

## Tests Added or Updated

- `user-server/internal/service/channel_conversation_type_test.go`：同一账号可接收两种会话；缺失类型不得由账号或上层入口猜测；同一 QQ 账号的单聊和群聊均能经过入站编排归档。
- `user-server/internal/service/channel_master_semantics_test.go`：渠道备注序列化、未传类型保留历史值、QQ／个人微信预设和接入服务不进入平台预设。
- `user-server/internal/model/menu_wiring_test.go`：独立渠道模块及原有叶子权限、路径归属。
- `user-server/internal/bridge/tenant_binding_test.go`：SageX 历史绑定保留真实平台，拒绝缺失或不支持的平台，保留普通绑定约束。
- `user-web/tests/unit/{menu_navigation,channels_module,channel_semantics}.test.js`：单入口投影不改变授权树、部分授权及深链、示例公司历史绑定可见、表单不提交固定会话类型、备注不丢失。
- `user-web/tests/unit/channel_options_new_channels.test.js`：将旧的“个人微信伴生未交付”文案断言更新为已核实的 SageX 接入与两种会话能力；公众号区分断言保留。

## Local Verification

- 已先观察会话类型、SageX 平台覆盖和租户上下文回归失败，再修改实现；最终定向后端回归 `internal/model`、`internal/service`、`internal/bridge` 三个包均通过，见临时日志 `channels-focused-go-final.log`。
- 限定前端回归 5 个实际测试文件、45 个用例通过（包含本轮渠道模块与此前全量回归暴露的 3 个超时文件），见 `channels-final-web-targeted.log`；此前 4 个受影响文件的 15 个用例也已独立通过。
- 最后一处绑定编辑按钮改用既有点分翻译键后，组件回归 4／4、相关文件 ESLint 及前端生产构建再次通过，见 `channels-verification-web-{component,eslint,build}.log`。
- 全量前端回归为 139 个文件中 136 个通过、3 个失败；1293 个用例中 1276 个通过、17 个失败，失败集中在 `badCase_render.test.js`、`browser_editor_dependency.test.js`、`clue_list_filter_push.test.js`。未修改这些非本任务文件以规避失败；3 个文件的定向串行回归已通过。
- 早期 Bridge 基线中的识别码断言和查询错误吞错问题已修复；最终定向 Bridge 回归通过，历史基线日志 `channels-baseline-lvuptqtw/baseline-tests.log` 仅作为修复前对照保留。
- 最新静态门顺序执行 Go build、vet、分层检查和 golangci-lint 均通过；完整包级 Go 测试的较早快照还包含 SageX 测试假对象 panic、若干 Bridge fixture 失败及当时尚未修复的身份用例失败。最终渠道定向回归通过；之后的全量 `make test-go` 被 OpenCode 服务重启中断，详见 [test.md](test.md)。
- `make audit` 在 i18n 孤立词条门停止：当前统计 4 条孤立 locale 词条，相比基线 0 变差。没有改动审计基线；具体全量回归状态见 [test.md](test.md)。
- `git diff --check`、跨层边界检查通过；四个渠道视图的 Impeccable 检查返回空问题列表。
- Graphify 刷新命令无法执行：当前 Python 环境缺少 `graphify` 模块。

## Manual Checks

2026-11-26 使用已有登录态，只读检查生产渠道页面及接口。页面最终实际展示 11 条记录，包括 `示例公司 / sagex-t18` 和 `QQ / qq`；账号接口存在关联示例公司的客服账号。较早观察到的空列表未在后续页面快照持续复现，因此不把它作为已证明的独立根因。生产消息同时含 QQ、个人微信和历史服务编码，确认存在渠道语义混杂。

同日使用本地临时 Vite 预览加载隔离树的新 SFC，页面明确标注“本地验收 · 示例数据 · 不连接生产接口”。逐元素证据保存在临时目录：`channels-ui-channels.txt`、`channels-ui-qq-edit.txt`、`channels-ui-binding-edit.txt`、`channels-ui-accounts.txt`、`channels-ui-account-edit.txt`、`channels-ui-account-saved.txt`、`channels-ui-permission-tabs.txt`、`channels-ui-identify-codes.txt`。证据覆盖单一导航入口、三页签、QQ 编辑、示例公司 SageX 绑定、账号备注保存、部分授权及识别码页；未执行重置、复制或生产写入。

生产页面仍是旧版本，这些证据用于确认现有数据与问题，不能视为新界面发布后的验收。本次没有重置或披露识别码。

## Deviations from Assessment

沿调用链补充修正了上层 `IdentifySpeaker` 的默认单聊，以及入站编排器的平台／会话类型参数混用。二者均属于“消息自身决定会话类型”的同一要求，并已纳入评估范围和回归测试。未扩展消息模型或改写历史记录。

## Follow-ups

- 最终验证结果以 [test.md](test.md) 为准；在全局门禁及发布条件满足前，不能声明线上修复完成。
- 生产历史服务编码消息是否迁移需另行核实关联后决定；本次仅修正后续消息解析与显示。
- 发布后按实际权限逐元素验证一个导航入口、三个页签、示例公司绑定及 QQ／个人微信单聊／群聊消息归属。
