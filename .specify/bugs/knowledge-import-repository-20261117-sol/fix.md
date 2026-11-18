# 修复记录

依据[assessment.md](assessment.md)，知识导入凭证service改为依赖同域专用仓储，不再引用根repository。新仓储复用SystemConfigKV模型与相同tenant/key，没有新增表。

- 新读写入口拒绝nil/System/取消/缺SaaS租户，OnlyTenant保留调用截止时间并明确过滤租户。
- `Save(nil)`返回错误，不发生解引用崩溃。
- 保留AES-GCM密文存储、公开AppID、透明读取、开发加密未初始化兼容及旧明文升级行为；数据库写入故障明确返回，不冒成功。
- 另外四份已获限定接管的文件仅gofmt/EOF清理；没有修改真实凭证、生产配置或发送策略。

真实RED：根仓储依赖、nil配置崩溃、System上下文被允许写入。目标修复后服务测试通过2.126秒；同域仓储防御与服务联合通过1.895/3.512秒。Go lint同CI版本2.10.0，0 issues。
