# Sidecar启动快照评估

Verdict: valid / medium。

## 根因

`internal/app/saas_config_params_wiring.go`的Sidecar provider每次调用参数中心读取地址，与该文件及`internal/etl/parser_seam.go`明确的“装配时读取一次，进程内不热切解析引擎”契约矛盾。解析开始与发送HTTP请求之间也会再次读取地址，运行期更新可能改变索引解析来源。

## 论证与决策

仅在装配时捕获`etl.parser_sidecar_url`，保留空地址禁用和重新启动/明确重新装配后生效的语义。图片描述URL/model、brain预算、文件URL有效期、知识上下文增强四条动态provider不改变。不改参数值、生产配置、解析协议、请求预算或数据库结构。

## Files likely to change

- `user-server/internal/app/saas_config_params_wiring.go`：仅Sidecar地址装配。
- 新`user-server/internal/app/saas_config_params_wiring_test.go`。
- 本目录fix/test及总进度。

## 验证

使用专用测试库的合成参数和两个本地HTTP解析服务，先证明参数更新后错误切换，再验证修复后同一进程仍用首次地址、明确重新装配才使用新地址。空地址启动后也不能被动态更新启用。测试恢复全局参数服务和provider，不使用生产数据库或外部解析服务。
