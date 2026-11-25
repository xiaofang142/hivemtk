# Fix: §3.30 LM-05/06/08/09/10/11/12/16 核心判据切片

- **日期**：2026-11-25
- **Assessment**：./assessment.md
- **状态**：**partial**（八项均交付核心契约/正反例，整项关闭需各剩余接线、真机或集成证据）

## 变更清单

| 项 | 文件 | 改动 |
|---|---|---|
| LM-05 | 新增 `service/lead_notification_normalize.go` + `_test.go` | 纯函数把已上报 `messages`/`historic_messages` 数组转为规范消息：源消息级幂等键、成员稳定标识（sender_key）、时间、历史/live 标记、未知方向保留、`parse_status`/截断字段与 `collection_incomplete`。**未改 `sagex_upload.go`**，未接 MessageEvent/Hub，真机采集能力缺口保留。 |
| LM-06 | 新增 `service/lead_channel_wiring_contract.go` + `_test.go` | 注册表 fixture + 资格投影测试；双模块独立分发契约：仅真实持久化入站进入发掘，AI 回声/系统通知/重复事件不触发。不要求真实渠道账号。 |
| LM-08 | `service/lead_detector_semantic.go` + 新增 `lead_semantic_axes_test.go` | 新增三轴分类纯函数：供需意图、相对产业链上下游、经营身份，保留多值与 unknown，否定/引用/转发不生成真实意图证据，证据带说话人/消息 ID；语义检测 Payload 追加 `semantic_axes`，不改既有 `matched`/`match_count`。 |
| LM-09 | 新增 `service/lead_tail_watermark.go` + `_test.go` | 按账号/会话的水位状态机：入队不等于完成，队列满保留请求，尾判最大等待有确定结果，迟到旧判定不回退水位。复用 BridgeLeadTask 思路，不新建第二套消费者。 |
| LM-10 | 新增 `service/lead_demand_key.go` + `_test.go`；`service/lead_mining.go` 的 `persistLead` 分桶段 | 需求键按主体/业务对象/方向独立分桶；未知主体 unknown 独立待核；证据版本比较防并发覆盖。分桶键在 `persistLead`（jd 判定之后）计算，避免引用未定义变量。 |
| LM-11 | `service/lead_group_identity.go`、`service/lead_discovery.go`（Process 非闸门段）+ 新增 `lead_member_identity_test.go` | 群成员身份与业务角色分离：普通发言只写 `member`，有证据的业务角色保持 demand/supply/broker/resource；空成员 ID 保留 unknown，管理员不等于买家。群 `grp:` OneID 未变。 |
| LM-12 | 新增 `service/lead_evidence_snapshot.go` + `_test.go`、`conversation_context_atomic_test.go`；`service/conversation_context.go` 新增 `AppendMessageAtomic` | 快照携带真实 MsgID/成员/方向/时间，按源消息去重；引用必须属于授权快照，合成 `history_N`/伪造 ID 拒绝；共享消息按 ID 幂等有界追加。未改权限范围。 |
| LM-16 | `service/inbox.go`（方向/群/完整性范围）+ 新增 `inbox_contract_test.go` | Hub 输出增加 `direction`/`is_group`/`group_id`，查询失败从 `ignored` 改为来源状态 `unavailable` vs `complete`；稳定游标（时间+ID）。未重写收件箱、未改前端推断逻辑。 |

## 施工边界遵守

- 未改 `sagex_upload.go`、`sagex_upload_test.go`、`controller/sagex_upload*.go`、`lead_react*.go`、`dispatcher_judge.go`、`router/router.go`、`auto_reply.go`、`customer_*.go`、`company.go`、`followup_record.go`、`channel_*.go`、`outbound_*.go`、`webhook_*.go`。
- 未改数据库 schema、未改 sagex/Android 仓、未改任何已存在的 `.specify/bugs` 目录。
- `lead_discovery.go` 的 `Enabled` 与 `Process` 前置闸门原样保留；`conversation_context.go` 只新增原子追加方法，不碰权限范围。
- 不 commit/push/发布/UI 真机点验。

## 修复过程中的关键纠正

1. 首次在 `process` 的 jd 判定前插入 LM-10 分桶代码导致 `undefined: jd`；已把分桶移到 `persistLead` 内（jd 判定之后），保留日志用粗粒度 account（`go build ./...` 现 rc=0）。
2. `lead_evidence_snapshot.go` 的 `h.ID > s.Watermark` uint/uint64 类型不匹配已改为 `uint64(h.ID) > s.Watermark`。
3. 测试 `TestConversationMessageAppend_DedupeAndBound` 初版对短 ID 切片越界 panic，已加长度判断并复跑通过。
4. `LeadEvidenceVersions.ShouldAppend` 同版本不同消息应拒绝（原实现误放行），已改为仅更高版本可推进。

## 未关闭判据（不虚报整项完成）

- **LM-05**：未接 `buildMessageEvent`/MessageEvent，未实现源消息级落库幂等；Android 通知载荷不等于会话数据库，改名/重装/权限关闭/静音/隐私隐藏均待真机。
- **LM-06**：fixture 基于注册表名字，未接真实渠道账号、TG/飞书/个微/QQ 的真实触发证据；CH-09/LEAD-03 公共分发与模块闸属其他任务。
- **LM-08**：未接 CRM-19 行业字典/标注集，未做召回/成本比较，未扩前后端筛选统计编码；`LeadRole` 单列仍保留兼容。
- **LM-09**：水位为内存态，未落 durable 任务表，未改 `BridgeLeadWorker`/`ProcessBridgeLead` 完成判据、未做进程重启对账。
- **LM-10**：只改线索分桶键与版本比较纯函数，未补商机归属、人工重开/合并、终态迁移与统计分母回查。
- **LM-11**：业务角色未写入 `MemberRoles`（仍由判定链使用），未接 SageX 稳定成员身份和个人客户关联。
- **LM-12**：`fetchHistoryDB`/`buildLeadJudgeContext` 不在可修改清单，合成 `history_N` 仍在现有链路；`AppendMessageAtomic` 未被生产路径调用，跨实例并发仍需 DB 事务集成。
- **LM-16**：未改 `ListByConversationContext` 的 1000 条上限与 sender/receiver 匹配（仅加来源状态字段），未改前端 `List.vue` 的 isOutbound 推断，未做 >1000 条翻阅真机验证。
