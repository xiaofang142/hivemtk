# Bug Fix: WhatsApp 线索交接

- Slug: whatsapp-lead-handoff
- Date: 2026-11-20
- Assessment: [assessment.md](assessment.md)

## Changes

修复两页对后端小写 `id` 的映射。选择通过当前 SPA 内存中的一次性随机标识交接，只保留 ID，绑定用户及租户，五分钟失效。消费后不持久化、不恢复旧 localStorage；保留已消费的路由标识以兼容 Layout 的 fullPath 组件 key，刷新或重放均为空选择。

接收方从当前接口解析姓名和号码，找不到任意所选联系人或读取失败即整批拒绝预选。跨列表页查找有界至 100 页，每页 1000 条。异步结果校验身份和加载代际，重置、换租户及离开后不恢复过时名单。

增加实际收件人名单与发送前确认。发送前及确认时重新读取联系人；姓名或号码改变须再次确认，缺失或读取失败不发送。发送使用已核对表单副本，防重复点击；取消、重置与浏览器重访不调用发送接口。既有沙箱 HTML 模板预览保留示例变量，导览明确其用途。新增文案覆盖现有九种语言。

## Local Verification

- 修复前真实组件回归 3 项失败，均证明后端小写 id 被丢弃：[red.log](evidence/red.log)。
- 全量 SPA 104 文件、1031 项通过：[full-vitest.log](evidence/full-vitest.log)。最终定向 3 文件、19 项通过：[focused-final.log](evidence/focused-final.log)。
- `npx eslint src --max-warnings 80` 退出 0，无输出：[eslint.log](evidence/eslint.log)。最终生产构建退出 0：[release-build.log](evidence/release-build.log)。
- 仓库总门禁 audit、lint、vet、test-go 均退出 0；Go 使用独立缓存完整重跑，159 条包级通过记录、零 FAIL，详见 [test.md](test.md)。首次共享 Go 缓存文件缺失的执行不计通过。
- Graphify 刷新实际报 `ModuleNotFoundError: No module named 'graphify'`，图谱未刷新。

## Delivery

链 D 已备份并同步 SPA，2026-11-20 10:25:26 +08:00 独立读取本地、服务器及公网九份资产，全部 HTTP 200 且 SHA-256 一致：[production.json](evidence/production.json)。同步保留旧 hash 资产及服务器 `.user.ini`。Mac openrsync 使用过滤删除参数时在实际替换前退出 2，回读确认旧 index 未变；随后普通增量同步成功，未执行删除。

构建 `113696881-1795141388751` 的前缀是构建时父提交，不作为本修复提交身份；修复树以 [源码摘要](evidence/tested-source-sha256.json) 绑定。回滚快照 `/www/wwwroot/hivemtk/user-web.snapshot-nav04-20261120-1024.tgz` 的 SHA-256 为 `3e2300b37cd215859af019c39263b1be4c85145f6b14e660b794b850b4132eca`。

## Scope

无后端、扩展或 SDK 发布。浏览器使用仓库内原始组件和本地合成接口，不发送生产消息。任务创建和第三方送达仍是不同状态；本项不证明真实第三方送达。
