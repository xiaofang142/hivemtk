# Sidecar装配快照修复

依据[assessment.md](assessment.md)，仅在`WireSaaSConfigParamSeams`装配时读取`etl.parser_sidecar_url`，将地址值捕获到原provider闭包。其余图片URL/model、brain预算、文件URL有效期和知识上下文增强四条动态provider保持原样。

不改变参数值、解析协议、120秒请求预算、索引逻辑或生产配置。空地址装配保持禁用；改变地址须明确重新装配或重启后生效。

真实RED `sh_1e85e94c200114nSgcs1ArMADk`：运行期更新参数切换解析器，且空地址装配后能动态启用。最小捕值修复后`sh_1e8606544001m3Zg7ZrHi9JwDQ`通过，app 1.617秒、ETL 0.663秒；详见[test.md](test.md)。
