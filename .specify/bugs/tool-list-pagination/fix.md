# Bug Fix: 工具列表服务端分页与可靠读取

- Slug: tool-list-pagination
- Fixed: 2026-11-20
- Assessment: [assessment.md](assessment.md)

## Changes

- 沿用列表接口的 page/page_size/list/total 契约，新增 20/50/100 条原生分页入口；总数使用服务端筛选 total，本页启用、禁用数明确标注。
- 筛选、重置及每页条数变化回到第一页；刷新保留当前页。状态保存后重读当前查询，让筛选移出、末页缩减及统计同步生效。
- 每轮读取绑定请求序号，过时成功、失败及 finally 均不能覆盖最新行、错误、总数或 loading；卸载使在途结果失效。
- 读取时清除可操作旧行，同时保留上一 total 直至新响应，防止 Element Plus 在翻页请求中把当前页自动重置为 1。最终读取失败清空总数并显示持久错误与刷新指引。
- 校验列表及非负安全整数 total；末页越界按新 total 回到有效页，最多三次读取；持续不一致明确报错，避免无限恢复或静默漏项。
- 九个语种同步新增八条文案；新增真实 Vue/Element Plus 组件回归与 localhost 专用验收夹具。夹具只替换测试构建的 API 导入，不进入生产路由、构建入口或认证链。

## Reproduction and Verification

- `tool-pagination-red.log`：原组件 11 项回归中 10 项失败，确认缺分页、总数错误及读取竞争；另一次修正隐藏 switch 选择器的测试错误不作为业务红灯。
- 首轮实现暴露 Element Plus 在 total 暂置 0 时自动回第一页，保留读取期间 total 后修正；`tool-pagination-unit-latest.log` 的 16 项组件回归通过。
- `tool-pagination-full-unit.log`：全量前端 99 文件、972 测试通过；全量 src lint exit 0。夹具原 console warning 已改为 stdout 输出，最终门禁另见 test.md。
- 首次生产构建被 i18n 完整性检查阻断：另外七个语种各缺八条新 key。返回修复阶段补全翻译，保持门禁阈值不变，再运行构建及前端回归；完整 Go 验收源码没有变化。
- ego lite 对真实组件逐元素完成 21 条翻页及第 21 项禁用、101 条切换每页 100 后第 101 项禁用、状态筛选、空结果、重置、503 读取失败及重试、末页并发删除后恢复第一页。
- 浏览器 `tool-pagination-browser-disabled21.json`、`tool-pagination-browser-disabled101.json` 均含 HTTP 200 的模拟接口事件，确认实际状态更新及随后重读；`tool-pagination-browser-read-failure.json` 保留 503 错误及无旧行；`tool-pagination-browser-delete-recovery.json` 记录 page 2 后重读 page 1。最终截图为 `tool-pagination-browser-final.png`，分页可见且无重叠。

## Scope

无后端接口、依赖、生产配置或迁移变化。浏览器操作仅作用于本地内存模拟工具，不证明第三方生产送达或真实生产工具修改；完整提交前门禁、生产构建和发布证据另记 test.md 及生产拓扑登记。
