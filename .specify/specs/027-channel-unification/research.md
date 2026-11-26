# Research: 027-channel-unification

## 决策与依据
1. **不另起渠道主数据**：既有 `channels`(088)、`channel_accounts`(089)、`channel_identify_codes`(090)、`channel_account_authorizations`(092) 已覆盖主数据需求；缺口在接线与前端。
2. **租户开关走既有 KV**：`service/channel.go` 的 `SetPresetEnabledForTenant` 已是唯一写口且 fail-closed；仅缺 Controller/Route。
3. **统一 API 组合端点**：前端当前零调用 `/channels/options`，新增组合端点可一次拿全渠道+账号，减少重复请求并成为唯一源。
4. **标签映射单点**：`constants/channel.js` 已有 27 项 code→汉字 + 旧码归一，但散落 19 视图重复定义；收敛为唯一静态字典 + Pinia 运行时 catalog。
5. **历史数据**：037/065 只归一码未打通 channel_id；本轮以「旧码归一 + 规范渠道补齐」为迁移落点，避免给 20+ 业务表加 FK 列的高风险改动。
6. **平台端**：version_type 默认空是生产故障根因（见 `docs/operations/PROD_TOPOLOGY_CHANGES.md:953-970`），需 DB/创建/UI 三处同时落 opensource。

## Alternatives considered
- 全业务表加 channel_id FK 并改 GORM 模型：覆盖更彻底但需改 20+ 表与写入路径，风险/成本过高，本轮不采用。
- 仅改前端字典不改后端：无法满足「API 统一」「历史数据修改」，不采用。
