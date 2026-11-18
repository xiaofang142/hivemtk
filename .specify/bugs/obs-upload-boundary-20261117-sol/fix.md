# 上传签发边界修复

依据[assessment.md](assessment.md)实施，不修改原管理端ObsConfigRepository接口、GetDefault、默认项设置、CRUD或数据库结构。

- 新增独立ObsUploadConfigRepository：拒绝未装配数据库、空/系统/取消上下文、缺失SaaS租户；仅按明确租户读取active默认项，并保留原加密字段解密兼容。
- 签发入口确认租户、对象路径及范围；大小限定为原访客端20MiB以内，期限1秒至1小时。拒绝绝对路径、父目录、查询/fragment、转义及控制字符。
- 公开地址兼容裸域名与HTTPS前缀，拒绝userinfo、非HTTPS、查询及fragment；中文对象路径正确编码。
- 保持七牛HMAC-SHA1、URL-base64、区域映射和原策略字段，不改实际存储配置或凭据。

真实RED：service 2.451秒，重复HTTPS协议、系统上下文和10个非法输入被签发；合法租户和协议对照通过。修复后补充回归：repository 2.222秒、service 1.515秒，包含原默认/加密契约。详见[test.md](test.md)。未调用生产上传接口或修改生产数据库。
