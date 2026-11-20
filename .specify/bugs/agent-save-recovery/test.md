# FA-AGENT-UI-03 · 部分保存恢复验收

- 日期：2026-11-20
- Result: verified（代码、真实 PostgreSQL 与本地受控浏览器验收；实际上线另见生产拓扑登记）

## 红灯与修复验证

原页面真实挂载测试首次 7 项失败、1 项通过，确认绑定拒绝后仍成功返回，以及创建回执丢失后缺少恢复协议。附加边界首次 4 项失败、14 项通过，暴露未知查询响应、错误回读 ID 和恢复期间修改编码的风险。修复后 UI-03 的 18 项与 UI-01/UI-02 的 24 项共同通过；全量前端结果见下表。

真实 PostgreSQL 使用专用 127.0.0.1:8217 / user_db_test，开启租户隔离，经过真实 Gin controller、service、repository。新增精确编码路由首轮红灯，修复后验证特殊字符精确匹配、未找到与查询故障区分、跨租户不可见、缺租户拒绝及数据库唯一约束。另将编辑器完整 payload 创建、更新后逐字段回读，空数组按现有 PostgreSQL null 表示归一化。没有连接生产数据库做故障试验。

## 最终检查

| 检查 | 结果 | 耗时 |
| --- | --- | --- |
| `make test-go` | exit 0；157 个包输出 ok，无 FAIL | 1044.9 秒 |
| app/service 全包 `go test -race` | exit 0；两个包通过 | 491.1 秒 |
| `make lint` | exit 0；0 issues | 33.2 秒 |
| `make vet` | exit 0 | 38.0 秒 |
| `go build ./...` | exit 0 | 16.2 秒 |
| layer-boundary | exit 0；1596 个生产 Go 文件，零违规 | 0.3 秒 |
| 纠正后完整 `make audit` | exit 0；静态审计通过 | 113.2 秒 |
| 纠正后全量前端 Vitest | exit 0；102 files / 1014 tests passed | 41.4 秒 |
| 纠正后 src ESLint | exit 0；使用现有 80 warnings 门 | 5.3 秒 |
| 纠正后生产构建 | exit 0；九语种键对齐，PWA 653 entries | 5.2 秒 |
| 组件、API、三组夹具及相关测试严格 ESLint | exit 0；max-warnings 0 | 2.3 秒 |

首轮 audit 因四条动态阶段翻译键被静态扫描识别为孤儿而失败。阶段到翻译键改为显式映射后，重跑完整 audit、全量前端测试、lint 和构建；未放宽扫描器或基线。初轮与纠正轮各自检查 6649 个索引文件，轮内变化均为零。两轮间仅 Edit.vue 和 fix.md 改变，4613 个 user-server 文件逐份 SHA-256 相同，因此 Go 全量、竞态、lint、vet、build 的已运行结果仍对应最终后端内容。

## 浏览器与恢复证据

真实 ego 浏览器使用 TaskSpace 25，在本地 127.0.0.1:18214 挂载真实 Edit.vue、Vue Router 和 ElementPlus，逐元素操作并留存每步快照。故障由独立内存 HTTP 夹具产生，未操作生产记录或真实第三方。

- 更新：主记录先保存，绑定 PUT 被受控 503 拒绝；页面显示主记录 ID 与绑定阶段错误，保留表单且不返回列表。重试只补绑定，主记录 PUT 仍一次；两资源回读一致后才整体成功。
- 创建：主记录 ID 2 已提交，绑定首次拒绝；页面保留 ID 与选择。重试后仅一个主记录、一次 POST，绑定变为所选 22，再显示成功。
- 创建回执丢失：POST 先提交再返回受控 503，页面不提示成功。重试通过精确编码和原配置恢复同一 ID；证据仍只有一次 POST、一个主记录。
- 单元边界另覆盖绑定提交回执丢失、主回读故障与不匹配、错误 ID、既有编码、未知查询响应、编码更改、重复点击、路由切换及卸载。UI-02 的未知或未改绑定继续跳过覆盖。

## 范围与未运行维度

这是可见且可重试的两资源协议，不是数据库原子事务。恢复状态保存在当前编辑器；重开后已有编码拒绝再次创建，须从列表编辑原记录。未增加迁移、跨浏览器恢复日志或并发版本锁，最终回读之后的其他合法更新不属于本次确认。

audit 明确 SKIP 覆盖率维度（无 coverage profile）及本机默认口令探针（8204 未启动）；不把跳过描述为通过，正式发布仍执行线上探针。Graphify 刷新命令因缺少 graphify 模块未执行成功，未声称知识图谱已更新。

## 本地证据摘要

日志与快照位于本次批准临时验收目录，以下 SHA-256 用于内容核对，不声称已上传 CI。

| 文件 | SHA-256 |
| --- | --- |
| agent-save-web-red.log | bf0707e148a3a280cfeb12b377e3f4d8270102e39a616e9c4df08097e73cd0af |
| agent-save-boundary-red.log | 8c4fac76cfa68efee683e3fb7ae3221773d677ccee022fb363d7be881aaaf4a3 |
| agent-save-final-test-go.log | 26e719af0642b42709060555c7f8daaef2893f94515135a824f52300811158ef |
| agent-save-final-race.log | f6163150b9b77eb3ce5abb5545196249c015f2e20caafdbfcba14d5557088fcb |
| agent-save-corrected-audit.log | accdb1dac073354c6a7afd21403d2c1c85b40c4cf5ae80e69db3f687a62f86a9 |
| agent-save-corrected-web-unit.log | d81f7c2aabed5869e687aa9ef7600ac5bb02f9b6b8f1a5e570a9d0d3c050cf0b |
| agent-save-corrected-web-build.log | 336f7533bf4d3156e6995787df01c579c4002a974c95ed6a96691d278bd7773d |
| agent-save-browser-update-partial.json | 15d80675436f41efd15303332a73c520ec14a0ce734956c1c653a64a3527c0f7 |
| agent-save-browser-create-recovered.json | 5c760f6e5b380ec5ca9c35cbfaafb0d10baef2b3834339d61ab63accd84254ec |
| agent-save-browser-lost-recovered.json | 4d92fba4475593c02de54c93179ff6c9a6b47946f6ffbb0c8a8f0043cfe64b52 |
| agent-save-corrected-source.json | c3607a5aeab4b8877940a30b6aa351a42e4f5b04e87377085bde69374b2077e4 |
