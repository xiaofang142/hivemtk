# Bug Fix: user-web 页面交互与数据审计（第一批）

- Slug: user-web-interaction-audit
- Assessment: ./assessment.md
- Status: partial

## Summary

修复批量预览误写/重复导入/筛选失效、权限读取失败放行、数据重复拆包、客户会话与订单异步串数据、文件上传与知识导入校验、登录重入与支付轮询生命周期问题。范围及逐项证据见 `user-web/docs/audit/user-web-interaction-audit/README.md`，全路由覆盖见同目录 routes.md。

## Changes

- 批量操作 API 与页面：严格校验筛选和匹配集合，dryRun 只读、确认快照、执行去重、选择文件不写入、真实预览字段与模板示例。
- permission store 与路由：请求共享、重置隔离、失败拒绝、管理员/个人页规则保留；运行时配置登录后读取且失败可回落。
- ToolFlow、ConfigParams、runtimeConfig、dictionary：按已拆包业务结果读取；工具流筛选和自定义意图加载接线。
- 客户会话：清空旧数据、画像字段映射、响应版本隔离、标签成功后更新、移除重复请求。
- 客户 360：已加载集合搜索分页修复、主详情失败拒绝、订单隔离、分层深链路径修复。
- 素材/知识导入：提交前校验、提交锁、部分成功保留、文件移除/预览失效、CSV 多行和 JSON 类型处理。
- 登录与收银台：Enter/校验前锁，串行轮询，订单和轮次隔离，关闭/卸载清理。
- 新增 7 个专项测试文件，扩展既有测试；新增仅本地合成数据 fixture；新增纯静态路由清单生成器。

## Local Verification

- 基线：102 files / 1014 tests passed。
- 中间全量：109 files / 1054 tests passed。
- 后续全量曾有 1 个测试失败（1054 passed）：消息/侧栏并发后旧测试按请求顺序投递消息，已改为按 URL 投递；原行为断言保留。
- 修正后专项：3 files / 20 tests passed。
- lint 正则转义错误与模板警告已清除，续跑退出 0。
- 最终构建退出 0，lint 退出 0；全量 1054 passed / 1 timeout，唯一超时文件独立复跑 6/6 通过。详见 test.md 和前端审计目录 verification.md，整批结论 partial。

## Deviations from Assessment

客户 360 后端实际上按会话分页后聚合客户、没有 keyword 参数，前端已加载集合过滤修复不能提供全量客户搜索；该项保持修复中，不声称闭环。本机无 API/PG/Redis/已认证浏览器会话，只有真实前端登录首屏及五个高风险组件的受控浏览器验证，不能替代 241 路由的逐交互/真实数据审计。未擅自搭建业务数据库或执行历史造数/删除 E2E。

## Remaining

真实后端逐页验收、全量客户分页/搜索、其余未验路由和静态发现保持未闭环。未提交、推送或发布。用户已有 `docs/verification/api-list.md` 修改保留。
