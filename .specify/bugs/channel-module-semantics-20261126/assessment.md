# 渠道模块与消息归属检查

用户要求：渠道独立为一个模块，不显示三个导航入口；QQ、微信等渠道均可承载单聊和群聊；SageX 是接入服务，不是渠道；检查四川同咨的数据。

## 已确认问题

- `SystemMenuTree` 将三个渠道入口放在系统设置/权限设置下，前端直接展示为三个菜单。
- 渠道与账号表单固定选择 dm/group/cs；`ChannelResolver.Identify` 在消息类型缺失时使用账号 Type，上层 `IdentifySpeaker` 也会把缺失类型默认成 dm，错误地替消息推断会话类型。
- 前端渠道常量遗漏 QQ；后端预设遗漏个人微信 personal_wx。
- 线上 2026-11-26 只读检查：`GET /api/channels` HTTP 200/code 0，包含 id=1/name=四川同咨/code=sagex-t18；`GET /api/channel-accounts` 包含所属渠道 1 的四川同咨客服账号。该名称是租户/账号名称，编码是接入服务标识，不是 QQ/微信平台。消息中台同时存在 qq、personal_wx、sagex-t18，证明确有归属混杂。
- Bridge 身份解析将识别码绑定的 ChannelCode 覆盖客户端平台，历史 sagex-t18 服务绑定可能把 QQ/微信消息错误写成服务编码。SageX 专用上传入口本身已按包名识别真实平台。
- 渠道列表 DTO 丢失备注，账号编辑未回填详情备注，存在保存覆盖风险。

## 修复方案与边界

保留原有三个 URL、菜单权限键及接口授权，新增独立渠道分组；前端只投影一个导航入口，原 URL 在模块内部切换，按原始授权显示。移除渠道/账号上的会话类型配置；消息识别必须使用消息自身类型。旧 Type 字段保留兼容，不再用于推断。补齐 QQ/个人微信与清楚的接入服务说明。Bridge 仅对明确的历史 SageX 服务编码允许 QQ/personal_wx 平台提示，继续校验识别码、租户、账号，普通渠道绑定不放宽。生产历史记录的迁移需另行核实关联，不直接猜测批量改写。

## 允许修改的源码与验证文件

- `user-server/internal/model/{menu.go,module_bindings.go,menu_wiring_test.go}`
- `user-server/internal/dto/channel.go`
- `user-server/internal/service/{channel.go,channel_master.go,channel_account.go,channel_resolver.go,channel_identity.go,channel_inbound_orchestrator.go,channel_conversation_type_test.go,channel_preset_mgmt_test.go,channel_master_semantics_test.go}`（沿调用链检查发现编排器把平台名误传为会话类型，需按消息 IsGroup 传递。）
- `user-server/internal/bridge/{tenant_binding.go,tenant_binding_test.go}`
- `user-web/src/utils/menuNavigation.js`
- `user-web/src/router/modules/channels.js`
- `user-web/src/views/channels/{index.vue,channel-manage.vue,account-manage.vue,identify-code-manage.vue}`
- `user-web/src/constants/channel.js`
- `user-web/src/config/onboarding/pageTours.ts`
- `user-web/src/i18n/locales/*.json`
- `user-web/tests/unit/{menu_navigation.test.js,channels_module.test.js,channel_semantics.test.js,channel_options_new_channels.test.js,identify_code_manage.test.js}`（完整测试发现旧个人微信文案断言仍要求“伴生未交付”，按已确认 SageX 接入能力同步更新该断言。）
- `router-list.md`、`user-web/router-list.md`；本目录 assessment/fix/test 报告。

## 验收

1. 完整及部分授权菜单均只有一个独立渠道入口，原始授权树不变，三个深链均可定位模块。
2. 同一账号可识别单聊和群聊，缺失/未知会话类型不再从账号推断。
3. 渠道/账号创建编辑不要求单聊或群聊；列表正常展示 API 返回；备注不会在编辑时丢失。
4. SageX 历史服务绑定不会覆盖明确的 QQ/个人微信平台，普通平台绑定及租户/账号检查保持有效。
5. 针对性前后端测试、构建、规范门禁，以及 ego-browser 逐元素页面证据。已有并行修改及环境失败单独记录，不冒称通过。
