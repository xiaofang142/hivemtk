# Bug Fix: 消息中台请求与详情状态

- Assessment: ./assessment.md
- Status: applied

## Changes

列表响应按版本隔离，验证list/total契约；失败清除旧行、总数和批量选中并常驻显示可重试原因。详情打开清空旧数据，按ID和版本校验，失败不回落列表行冒充成功；关闭废弃在途响应。统计失败显示错误。已读成功后刷新列表及统计，遵守筛选集合。推送在异步表单校验前上锁，拒绝重复提交。

## Verification

- 新增messageHub_request_state.test.js：5/5通过，覆盖列表顺序、失败重试、详情隔离、推送校验阶段去重、已读刷新。
- 既有messageHub_unknown_status_render.test.js：3/3通过；首次联合7项通过，补推送用例后新专项5项通过。
- lint exit0，无错误/警告；前端build exit0，25.38s；git diff --check通过。
- 真实API8401前端8212：消息统计41/27/14/13与API一致，两页各20行与真实ID核对。搜索/详情补验遇工具超时，未标通过。
- 看板曾遭ECONNREFUSED，API恢复后错误横幅自动消失；真实一小时窗口0与DLQ0一致。

## Limits

新写入锁仅模拟接口测试，无真实渠道推送/撤回。真实页面所有交互未完成，后端新客户分页也未装载运行实例。本报告不代表全站验收。
