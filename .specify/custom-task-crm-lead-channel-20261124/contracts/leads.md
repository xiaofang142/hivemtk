# API Contract: 线索发现（第二部分）

> 路由口径（2026-11-24 核对）：权威源为 `user-server/internal/router/lead_routes.go`（`auth.Group("/manage/leads")`）
> 与 `lead_mining_agent_routes.go`。契约早期草稿列出的 judgements 列表/详情/重判、clues CRUD
> 在路由中未找到，一律标「待核对」，不臆造路径。

## 规则配置（三粒度）
- `GET /api/manage/leads/rules` — query: scope(global|channel|account), scope_id
- `POST /api/manage/leads/rules` — body: scope, scope_id, keywords[], semantic_keywords[], system_description, enabled
- `PUT /api/manage/leads/rules/:id` — **待核对**：路由未找到；实际更新走 `POST /api/manage/leads/rules`（SaveRule）
- `DELETE /api/manage/leads/rules/:id`

## 通知路由
- `GET /api/manage/leads/notify-routes` — query: channel_account_id
- `POST /api/manage/leads/notify-routes` — body: channel_account_id, notify_user_ids[], notify_types[]
- `PUT /api/manage/leads/notify-routes/:id` — **待核对**：路由未找到；实际删除走 `DELETE /api/manage/leads/notify-routes/:route_id`
  （无对应 PUT 路由）

## 判定与命中
- `GET /api/manage/leads/judgements` — 列表，含三路证据与二次 LLM 结论（**待核对**：路由未找到）
- `GET /api/manage/leads/judgements/:id` — 详情（hits + 上下文消息）（**待核对**：实际只找到 `GET /api/manage/leads/judgements/:judgement_id/hits`，仅命中列表）
- `POST /api/manage/leads/judgements/:id/rejudge` — 手动重判（使用最新上下文）（**待核对**：路由未找到）

## 线索归档
- `GET /api/manage/leads/clues` — 列表，按数据权限过滤（**待核对**：路由未找到）
- `POST /api/manage/leads/clues` — 归档（用户信息 + 线索信息 + 关联客户）；实际路由为 `POST /api/manage/leads/archive`
- `GET /api/manage/leads/clues/:id` — **待核对**：路由未找到

## 消息/客户列表（配置人员权限内）
- `GET /api/manage/leads/messages` — 仅权限内渠道账号的消息列表
- `GET /api/manage/leads/customers` — 仅权限内渠道账号的客户列表

> 平台通知（TG/飞书）调用成败以业务码为准（宪法 IV），送达结果写入 notify_routes.delivery_records。
