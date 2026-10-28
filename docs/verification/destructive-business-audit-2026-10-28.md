# 破坏性业务测试与数据还原（2026-10-28）

## 1. 原则

- 测试前对 `user_db` / `platform_db` 做完整物理备份：
  - `qa-backup-20261028/user_db.dump`（67MB）
  - `qa-backup-20261028/platform_db.dump`（1MB）
- 允许创建/修改/删除业务数据，测试后整库还原，做到“覆盖业务、不留痕迹”。
- 不触发真实外发：短信/邮件/WhatsApp/Telegram 发送类提交一律排除；平台确认收款/打款等只改本地测试库。

## 2. user-web 破坏性覆盖

- 用统一探针扫描 108 个含「新建/新增/创建/添加」入口的页面：
  - 点击创建入口 → 填表（文本/数字/URL/邮箱/JSON/下拉）→ 提交 → 抓 `Network` 请求与响应 → 页面/API 双重校验。
  - 对创建成功且能定位行的页面继续点删除并确认。
- 成功创建并验证的实体（节选）：

| 实体 | 数量 | 验证 |
|---|---|---|
| AI 智能体 | 9 | 列表/详情/API 均含 QA 名称；详情 → 更新 → 启停 → 删除 → 404 全链路通过 |
| 短链 | 8 | 创建成功；列表/DB 一致；删除后 DB=0 |
| 知识库 | 3 | 列表返回 3 条 QA 知识库 |
| 会话标签 / 快捷回复 / 社群 | 4 / 3 / 3 | 列表与 DB 一致 |
| 卡片（抖音/快手/小红书/闲鱼/TikTok） | 3/1/1/1/1 | 创建接口 code=0，列表可查 |
| 邮件/短信/WhatsApp 草稿 | 各 1 | 草稿保存成功，不触发发送 |
| Feature Flag / 自定义报表 / 集成账号 / 外部连接器 / GEO 站点 / 竞品 / LLM Provider / RAG 产品 | 各 1–3 | 创建成功，GET 可回读 |

- 校验失败类（400）均为必填/格式校验，属于正常业务拦截，例如：
  - 浏览器任务 `url 仅支持 http/https`
  - FAQ 缺 `agent_id`
  - QQ webhook 路径不合法
  - 活码域名不可用
  - Telegram Token 格式不对
  - 短信任务缺草稿/手机号
  这些页面均在 UI 显示对应字段错误，没有把校验失败吞成成功。

### 2.1 发现并修复的业务缺陷

**短链更新接口恒 400（生产路由与实现不一致）**

- 现象：`PUT /api/shortlink/update` 带 `{"id":276,...}` 恒回 `400 无效的ID格式`。
- 根因：生产路由 `/shortlink/update` 没有 `:id` path 参数，控制器却只从 `ctx.Param("id")` 取 ID，永远为空；历史测试用 `/shortlinks/:id`，所以测试全绿而线上不可用。
- 修复：`internal/controller/short_link.go` 支持两种形态——path 有 id 时校验一致性，没有时使用 body.id。
- 回归：新增 `TestShortLinkController_Update_ProductionRouteUsesBodyID`；真 API 更新后 GET 回读到新标题/URL；删除后 DB 记录为 0。

## 3. platform-web 破坏性覆盖

| 流程 | 操作 | 验证 |
|---|---|---|
| 系统参数 | `asset_market/batch_operation_limit` 100 → 101 → 回默认 | GET 回读 101；reset 后回 100 |
| 站内信 | 发送 `QA_PLATFORM_<ts>` | 列表出现该消息 |
| 模块目录 | 创建 `qa-mod-<ts>` → 详情 → 改名/改价 → 删除 | 创建/更新成功；删除后详情 404 |
| 资产审核 | 取 pending 资产 149 → 通过 | DB `status=approved` |
| 订阅配置 | GET → 原值 PUT → GET | 值往返一致 |
| 官网联系信息 | GET → 原值 PUT → GET | 值往返一致 |

## 4. 不留痕迹验证

还原方式：停服务 → 终止连接 → `pg_restore --clean --if-exists` 恢复两份备份 → 重启服务。

- 全库扫描 `%QA_AUTO_%`：`TOTAL QA = 0`
- 短链 277/276 等测试记录：不存在
- 平台 `QA_PLATFORM_%` 站内信：0
- 平台资产 149：恢复为 `pending`
- 基线复核：user 通知总数 1163、platform 商户 59，与测试前一致

## 5. 限制

- 真实短信/邮件/WhatsApp/Telegram 外发未触发（避免真实触达）；这些页面验证到“保存草稿/任务创建/参数校验”层。
- 支付只验证本地配置与订单读取；未发起真实网关支付。
- 备份文件位于工作区 `qa-backup-20261028/`，不属于代码仓库。
