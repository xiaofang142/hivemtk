# Fix: 识别码命中后的渠道与租户边界

- Status: applied；关联 CH-20261125-07、LEAD-20261125-03。
- 修改限定在 assessment 的 `channel_resolver.go`、`channel_resolver_boundary_test.go`。
- 私有解析方法同时返回已校验的渠道行；共用账号/渠道主键、租户及渠道码校验，传播不可用仓储和查询错误，取消尽力查询后的无渠道身份返回。`ErrChannelIdentityInvalid` 区分已命中但不完整的主数据与健康未命中，防止后者的 legacy 兼容吞掉故障。
- `ResolveByAccountRef` 先核对可信租户，再对识别码、数值 ID、名称各分支校验渠道 Code/Type；数值查询复核实际 ID，不再二次读取渠道后忽略错误。满页搜索无论是否含精确匹配，均不宣称身份唯一。
- `ResolveTenant` 仅对健康未命中保留历史兼容；`ResolveTrusted` 复用同一渠道结果检查启停，复读账号还须保持 ID/租户/渠道一致。未绑定租户的引导入口仍可由可信识别码确定归属，外部账号引用在该归属内复核。
- RED：原实现接受错误平台/租户、缺租户和渠道故障；Webhook 将渠道故障回退为 tenant=1。附加满页用例还复现单个可见精确匹配被误认为唯一。首轮修复后原有渠道与闸门定向回归通过；最终验证见 test.md。
- 无 allowlist 外源码修改；未修改旧 bug 记录和并行脚本，无 DB/外部渠道/模型/部署操作。
