# Contract: Channel Catalog API

## GET /api/channels/catalog
响应（envelope `code/message/data`）：
```json
{
  "channels": [{"id":1,"code":"wecom","name":"企业微信","label":"企业微信","enabled":true}],
  "accounts": [{"id":9,"channel_id":1,"channel_code":"wecom","name":"主账号","identify_code":"cid_xxx","status":1,"label":"主账号"}]
}
```
- 仅返回当前租户已启用渠道、已启用账号；开源版过滤国内渠道；缺租户 4xx。

## PUT /api/channels/:id/tenant-enabled
Body `{"enabled":true|false}`。预设渠道写租户 KV，非预设渠道写租户行；缺租户 fail-closed。

## GET /api/channel-accounts/managed
Query: channel_id/status/keyword/limit/offset。含停用账号，total 与过滤口径一致。

## PUT /api/channel-accounts/:id/managed
Body 为指针字段（nil=不改）；含 name/remark/self_identify_name/type/channel_id。

## PUT /api/channel-accounts/:id/status
Body `{"status":0|1}`。

## GET /api/channel-accounts/authorization-options
Query `keyword`。返回当前租户可授权账号（`ChannelAccountOption`），复用账号仓储只读口径。

## GET /api/channel-accounts/:id/authorizations
`:id` 为被授权员工 ID；返回该员工在当前租户下的授权配对（`user_id/channel_account_id/visible_industry_scope/role`）。

## POST /api/channel-accounts/:id/authorizations
Body：`{"accounts":[{"account_id":9,"industries":["教育"],"role":"viewer"}]}`。
覆盖式保存；`role` 缺省 `viewer`，仅允许 `viewer|owner`；空授权/重复账号/跨租户返回 400。

## DELETE /api/channel-accounts/:id/authorizations/:account_id
撤销该员工的单条账号授权；权限解析每次重新读表，撤销即时生效。

以上四个端点复用 `ChannelAccountAuthorizationService`，不新建第二套授权表；缺租户 fail-closed。
