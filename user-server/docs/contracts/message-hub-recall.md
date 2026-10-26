# 消息撤回合同（`POST /api/message-hub/:id/recall`）

> **状态**：已实现（2026-10-26，OPEN_ITEMS B2 收口）
> **代码**：`internal/controller/message_hub.go`（Recall）· `internal/service/message_hub_recall.go`（状态/幂等/分类）
> · `internal/app/integration_reach_adapter.go`（RecallHub，平台分派）· 迁移 `v3.86.0`
> **本文件不是生成物**（`cross-repo-wire.json` 那份才是），手写维护。

---

## 1. 谁调谁

这条链**没有平台端（`hivemtk-platform`）参与**：

```
运营（user-web 消息中心）
   │  POST /api/message-hub/:id/recall
   ▼
user-server  ── 查 message_hub 行 ──► 判方向/窗口/渠道
   │
   ├─ telegram  POST https://api.telegram.org/bot{token}/deleteMessage
   ├─ feishu    DELETE /open-apis/im/v1/messages/{message_id}
   ├─ wecom     POST /cgi-bin/message/recall
   ├─ whatsapp  ✗ 无主动撤回接口
   └─ 桥接渠道（抖音/快手/小红书/tiktok/闲鱼） ✗ msgID 是合成键，不是平台消息标识
```

常见的误判是把 B2 读成"平台端要反向调用 user-server 撤回已发布内容"。不是：
`hivemtk-platform` 从不调用 user-server（账户/心跳/订单/模块/LLM 代理都是 user-server 主动出站），
而"撤回一条已经发到微信/飞书/TG 的消息"必须由持有该渠道凭据的一侧直接打渠道 API —— 也就是本仓。
所以**合同是同进程内的一条 HTTP 契约 + 平台渠道的外部 API 契约**，不需要跨仓反向调用，
也不进 `cross-repo-wire.json` 的类型集。

---

## 2. 请求

| 项 | 值 |
|---|---|
| 方法/路径 | `POST /api/message-hub/:id/recall` |
| 鉴权 | 登录态 + `RequireAdminMiddleware`（与 `/message-hub/*` 其余口同一道闸：全量跨账号消息面） |
| `:id` | `message_hub.id`（**行主键**，不是 `msg_id`） |
| body | 无 |

为什么按主键寻址：`msg_id` 的唯一索引是 `(platform, msg_id, conversation_id, direction)` 四元组，
单按 `msg_id` 查会挑到同键的入站行或另一会话行，撤回就会撤错对象。

---

## 3. 响应

```jsonc
// 200 成功（或本来就已经撤过）
{
  "code": 0,
  "message": "撤回成功",
  "data": {
    "id": 1024,
    "channel": "telegram",
    "recalled": true,
    "already_recalled": false,       // true = 本次没有出网，这一行此前已是终态
    "recalled_at": "2026-10-26T18:40:12+08:00"
  }
}
```

失败一律 `{code, message}`，`message` 带**可展示**的原因（含平台返回的业务码原文）：

| HTTP | 含义 | 运营下一步 |
|---|---|---|
| 400 | 入站行不是撤回对象 | 无（客户发来的消息我们没有平台凭据删） |
| 404 | 这一行不存在 | 刷新列表 |
| 409 | 渠道无主动撤回通道（WA / 桥接渠道） | 去平台后台手工删 |
| 409 | 已超过平台撤回窗口 | 无（窗口过了就是过了） |
| 502 | 出网失败（网络/平台业务码拒绝/凭据缺失） | 可重试 |
| 503 | 撤回端口未装配（无 DB 句柄） | 运维查装配日志 |

409 与 400 分开不是洁癖：`400` 是"请求写错了"，`409` 是"请求没错，但这个对象/这个时点做不到"。
两类给到运营的动作完全不同。

---

## 4. 幂等（本合同的硬约束）

幂等判据是**数据**，不是请求头：

- `message_hub.recalled_at` 非空 ⇒ 终态。再调一次直接回
  `200 {recalled: true, already_recalled: true}`，**不再打渠道 API**。
- 并发两次请求由条件更新裁决
  （`UPDATE ... WHERE id = ? AND direction = 'outbound' AND recalled_at IS NULL`）。
  输的那一笔拿到 `RowsAffected = 0`，按 `already_recalled` 回成功 —— 两边结论一致，
  但**只有一个请求真的出网**。

为什么必须本地判，而不是靠平台报错来判：渠道对重复撤回的回应并不一致 ——
TG 回 `message to delete not found`、飞书回"消息已撤回"、企微回"消息不存在"。
把这三句话当成"失败/成功"来判，等于把幂等交给对方的心情；而且它们与"这条消息本来就不是我们发的"
在错误码上无法区分。

失败**不是**终态：`recall_error` 只记最近一次原因、`recalled_at` 保持 NULL，
所以窗口内可以重试；成功时 `recall_error` 被清空。

---

## 5. 数据形状（迁移 `v3.86.0`）

| 列 | 类型 | 语义 |
|---|---|---|
| `recalled_at` | `timestamptz NULL` | 平台确认撤回的时刻（终态判据） |
| `recall_error` | `varchar(200) NOT NULL DEFAULT ''` | 最近一次失败原因；成功即清空 |

**不回填**：历史行 `recalled_at` 恒为 NULL —— "不知道撤没撤过"不许写成"没撤过"以外的任何值。

**不改 `status`**：`status` 是发送状态机（`pending → sent/delivered/send_failed`），
撤回是"发出去之后有没有撤掉"的另一个维度。挤进一列会抹掉投递事实，
并让 `UpdateDeliveryStatus` 的乱序回执保护（`send_failed` 不回翻）失效。

---

## 6. 复算

```bash
cd user-server
go test ./internal/service -run TestMessageHubServiceRecall -count=1        # 幂等/分类/落格/重试
go test ./internal/repository -run TestMessageHubRecallRepository -count=1  # 条件更新的乐观锁
go test ./internal/migration/migrations -run TestMessageHubRecallMigration -count=1  # 补列/幂等/不回填/拒绝删列
go test ./internal/app -run Recall -count=1                                  # 渠道分类
bash ../scripts/menu-route-role-audit.py --check-go internal/model/menu_capability_routes.go --out /tmp/mra  # 前端调用点都能落到 live 路由
```
