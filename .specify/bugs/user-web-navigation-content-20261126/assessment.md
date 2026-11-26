# 用户端导航与内容审查修复评估

Status: valid

用户授权：全部自动处理，更新文档与前后端，处理线上已有鉴权和模块依赖。

依据：2026-11-26 对当前源码全量审查（93 路由模块、267 模块路由、202 个潜在菜单入口）；已通过代码级复现确认指标单位错误、探测来源误导、失败预览显示成功和无标记示例大屏。真实业务页面登录态已过期，未声称完成线上 UI 验证。

## 根因与修复边界

- 菜单展示直接继承历史授权树，业务分组与角色/模块授权耦合。按用户补充要求直接修正后端权威树，并同步角色上界、模块绑定/依赖与前端消费；不以展示投影替代后端修复。
- 销售驾驶舱把调用次数和账号数转为 QPS，丢弃账号状态并虚构错误率。
- GEO 主动探测携带 AI User-Agent，统计与文案未区分探测和外部抓取。
- llms/robots 预览将异常文案存入内容，成功标签用非空内容判断。
- 大屏画布随机/固定示例值缺少明确标示。
- CRM/线索治理子页缺少可发现入口，TikTok 详情缺少导航归属，单子分组增加层级。

## 允许修改面

- `user-web/src/utils/menuNavigation.js`、导航组件、Layout、permission store（仅修复必要处）、i18n、onboarding、路由台账及相关回归测试。
- `user-web/src/views/{SalesCockpit,geo,dashboardScreen,crm,leads,email,system,approvalTask,badCase,orderDraft,aiInsightDraft,knowledgeBase,KnowledgeWorkspace}/` 中审查问题及对应 API/测试。
- `user-server/internal/geo/` 的探测来源契约与回归测试；`internal/{model,repository,service}/` 的菜单/模块回归测试及驾驶舱口径（不扩大权限）。
- 当前修复说明、导航架构说明、发布台账。

不触碰其他会话在途文件，不修改扩展，不迁移线上菜单授权、不购买模块、不发送业务消息。

## 验收

重组后不丢失已有业务功能，分组权限不意外放宽业务访问；模块关闭与受限角色仍 fail-closed；旧 URL 可用。四项内容错误有回归证据；新增入口遵从服务端权限。前端测试/lint/build、Go 分层/build/vet/相关测试及发布门禁通过后，以现有发布器发布 API 与 SPA，记录实际证据。
