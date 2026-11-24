# API Contract: 渠道/渠道账号/识别码（第四部分）

> 路由口径（2026-11-24 核对）：Phase6 裁决沿用 Phase2 实际路径，`user-server/internal/router/channel_routes.go`
> 与 `lead_mining_agent_routes.go` 是权威源。契约早期草稿里的 `/api/manage/channels`、
> `/api/manage/channel-identify-codes`、`/api/manage/agents/lead-mining` 均未落地，已按下表修正。

## 渠道管理（预设表驱动）
- `GET /api/channels` — 全部预设渠道 + enabled 状态
- `POST /api/channels` — 新增预设渠道（一次配置全系统生效）
- `PUT /api/channels/:id` — 开启/关闭、改名、改类型
- `DELETE /api/channels/:id`
- 实际另有 `GET /api/channels/options`、`GET /api/channels/:id`（契约未列，供参考）

## 渠道账号管理
- `GET /api/channel-accounts` — 列表（按数据权限）
- `GET /api/channel-accounts/options` — **统一下拉数据源**（FR-029，系统内所有选择统一调用本端点）
- `POST /api/channel-accounts` — body 含 name/remark/self_identify_name/type/auto_reply_agent_id/lead_mining_agent_id/owner_user_ids/data_scope
- `PUT /api/channel-accounts/:id`
- `DELETE /api/channel-accounts/:id`
- 实际另有 `GET /api/channel-accounts/:id`（契约未列，供参考）

## 唯一识别码（替代 bartoken）
- `GET /api/identify-codes` — 列表
- `POST /api/identify-codes/:channel_id/rotate` — 为渠道生成/重置识别码（version+1）；实际无独立 `POST /api/identify-codes` 端点，生成与重置统一走此路由
- 实际另有 `GET /api/identify-codes/:channel_id`（契约未列，供参考）
- 三条链路（bridge / 统一 API 上报 / hook）对外读取统一通过 `ChannelResolver.Resolve(code)` 解析租户与账号，不再识别 bartoken。

## 线索发现智能体（改名）
- `GET /api/manage/lead-mining-agents`
- `POST /api/manage/lead-mining-agents`
- `PUT /api/manage/lead-mining-agents/:id`
- `DELETE /api/manage/lead-mining-agents/:id`

## 统一自他识别（内部方法/对外契约）
- `ChannelResolver.IdentifySelf(channelAccountID, conversationID, message) -> self|other`
  - 单聊：按双方账号判断；群聊：按成员角色判断（FR-030）。
