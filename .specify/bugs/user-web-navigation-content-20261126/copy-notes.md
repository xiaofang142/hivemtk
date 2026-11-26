# 用户正文文案修复交接

首轮子任务修改授权的 21 个页面组件与 `businessCopy.zh.json`、`businessCopy.en.json`，未 commit/push。未修改 `crm/List.vue`、其他子任务页面、导航、onboarding 或全局 catalog。后续按主助手授权，仅补改两份渲染测试及本文说明。

## 改动

- 待办、坏例、订单草稿的不可用告警改为业务影响与联系站点管理员，移除 503、DB 句柄、装配开关、重启等实现说明；保留 `unavailable`、`assembled === false`、`!me` 及各操作权限的既有判断。告警明确读取失败或未读取不代表队列已清空。
- 身份缺失提示改为「当前身份未能确认」，说明哪些操作不能执行并引导联系站点管理员，不再承诺重新登录可以解决。
- 订单草稿卡片改为已启用／服务异常／暂不可用／临时保存的业务状态；计数按现有状态名显示；未读取数量单独提示。服务告警继续展示，临时保存时解释无法保证恢复。临时订单号仍为 warning，明确正式订单未创建及补建要求；取消／确认／编辑逻辑未改。
- CRM、授权线索、洞察草稿、客户导入页面移除 US1、US2、PR-6、PR-7 等内部编号。洞察生成实际调用 `GenerateStub`，因此用「根据客户资料生成，未使用 AI 模型」解释真实能力，已知 `stub_readonly` 的生成来源不再直接展示内部版本号；证据和动作数据仍保留。
- SMTP 文案统一为「发件账号」，覆盖新增、编辑、表格、表单、校验及删除确认。邮箱参考页增加「邮箱服务商配置参考」标题与用途说明。引导页删除虚构支持地址，改为联系站点管理员。
- KnowledgeWorkspace 中使用 `ragProductConfigAPI.listProducts` 的 8 个页面统一选择器、表单、筛选与校验文案为「RAG知识库」，说明它是文档导入与检索使用的文档集合。保持 `product_id`、ID 类型、接口及重名区分逻辑不变；`Connectors.vue` 与 `CrawlerSources.vue` 使用另一套知识库模型，未改动或合并它们。
- 新文案均使用 `useI18n`，仅新增两份 `businessCopy` 中英资源，无新增文案镜像测试。

## 集成状态

1. 主助手已在 catalog 为 zh/en 接入 `businessCopy` namespace，两份资源顶层不自带 namespace 包装；本子任务未改 catalog。
2. 已按后续授权在 `approvalTask_render.test.js` 和 `badCase_render.test.js` 的挂载插件中安装真实项目 i18n，每例固定中文语言。保留原有认领、审批、身份只读、打标条件、提交内容、重复提交抑制和错误恢复等行为断言，无翻译 mock 或新增文案镜像测试。
3. 更新 `badCase_render.test.js` 原有身份与 503 行为用例中的业务提示断言：身份未确认需联系管理员；服务不可用时空列表或零值不代表队列已处理完毕。未改产品组件或 locales。

## 验证

- `git diff --check`：通过。
- 现有相关回归首次运行：9 个文件中 7 个通过，66 个用例通过；2 个渲染文件共 23 个用例因未安装 i18n 插件而在 setup 阶段失败，尚未执行到业务行为断言。通过文件为 `approvalTask_actions`、`badCase_actions`、`api_orderDraft`、`knowledge_batch_import`、`knowledge_batch_import_validation`、`knowledge_batch_import_feedback`、`email_preview_isolation`。
- 补齐真实 i18n 夹具与旧提示断言后，同一组 9 文件回归全部通过：`Test Files 9 passed (9)`、`Tests 89 passed (89)`，耗时 105.01 秒（08:01:19 启动）。两份渲染文件原有 23 个行为用例全部执行通过。
- 测试补修使用的完整回归命令：`cd user-web && npx vitest run tests/unit/approvalTask_actions.test.js tests/unit/approvalTask_render.test.js tests/unit/badCase_actions.test.js tests/unit/badCase_render.test.js tests/unit/api_orderDraft.test.js tests/unit/knowledge_batch_import.test.js tests/unit/knowledge_batch_import_validation.test.js tests/unit/knowledge_batch_import_feedback.test.js tests/unit/email_preview_isolation.test.js`。
- `cd user-web && npx eslint tests/unit/approvalTask_render.test.js tests/unit/badCase_render.test.js --max-warnings 0`：exit 0，输出为空。两份测试及 notes 的 `git diff --check` 通过。
- 最终指定 21 个文件的 `npx eslint ... --max-warnings 0`：exit 0，零错误、零警告。首轮发现的 Playground 循环变量 `t` 遮蔽翻译函数及已不再使用的 i18n import 均已修正。
- 全量 `cd user-web && npx eslint src --max-warnings 80`：exit 0，输出为空。
- Vue SFC 和消息编译检查：`PASS: 21 Vue components compile; 54 bilingual messages compile; 99 businessCopy references resolve.` 中英文键完全一致。
- 未运行浏览器 UI 点验，未声称真实页面验证通过。
- 按环境规则尝试 graphify 重建，命令报 `ModuleNotFoundError: No module named 'graphify'`；未变更本机环境。
