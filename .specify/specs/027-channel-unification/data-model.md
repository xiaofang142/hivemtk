# Data Model: 027-channel-unification

- **Channel**（既有）: id, tenant_id(0=全局预设), code(全局唯一), name, enabled。
  - 规则：预设渠道启用状态 = 全局行 enabled && 租户 KV `channel_preset:<code>`（缺省 1，脏值 0）；非预设 = 租户行 enabled；国内渠道受版本门控制。
- **ChannelAccount**（既有）: id, tenant_id, channel_id, name, identify_code, status(1 启用/0 停用), owner_user_ids, data_scope。
  - 规则：一渠道多账号；identify_code 全局唯一；业务下拉仅 status=1。
- **ChannelIdentifyCode**（既有）: channel_id, account_id, code, version(CAS), prev_code。
  - 规则：账号级重置只影响指定账号。
- **Merchant.version_type**（平台仓）: opensource(默认)/custom；存量空按 opensource 展示与下发。
- **历史业务表 code**：归一映射到规范 code 后由统一字典显示中文。
