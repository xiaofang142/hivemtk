# 访客上传凭证边界评估

Verdict: valid / high。

## 取证

`BuildQiniuUploadToken`直接读取默认对象存储配置，未显式拒绝空上下文、系统豁免或缺失SaaS租户；默认仓储在数据库未装配时也可能崩溃。签发入口未检查对象key、负数大小和期限。公开URL固定拼接`https://`，已含协议的配置会产生重复协议。

## 论证与决策

- 不改真实凭证、存储默认行、七牛HMAC-SHA1/URL-base64协议、上传区域映射或数据库结构。
- 增加独立受控仓储读入口，显式绑定租户并只选该租户的active默认行；原管理端仓储接口及加密读写契约保留。
- 上传签发拒绝未知上下文及不合法key、大小或期限。上限镜像现有唯一访客调用方的20MiB/1小时，不扩大许可。
- 公开URL支持原无协议域名和已有HTTPS域名；拒绝其它协议、userinfo、查询与fragment，不通过网络验证域名。

## Files likely to change

- `user-server/internal/service/obs_upload_token.go`
- 新`user-server/internal/service/obs_upload_token_test.go`
- 新`user-server/internal/repository/obs_config_upload.go`
- 新`user-server/internal/repository/obs_config_upload_test.go`
- 本目录fix/test与总进度；不修改原`GetDefault`或管理端更新/设默认逻辑。

## 验证

先真实专用PG与合成AK/SK复现未知上下文和重复协议，保正常签名与跨租户正向对照。随后检查大小/期限/key、缺配置、数据库缺失、旧默认/加密回归。所有测试只使用127.0.0.1:8217，生产库与上传接口不调用。
