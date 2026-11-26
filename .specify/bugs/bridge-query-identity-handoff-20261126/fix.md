# Fix: 鉴权后保留识别码交接

- Result: partial；本机中间件交接修复，关联入口/渠道任务继续开放。
- 公共 `BridgeIngressGuard()` 仍使用原渠道识别码仓储候选源；内部窄函数接收候选源，允许测试不访问数据库。每个请求仍读取候选，header 优先级、常量时间比较及拒绝状态不变。
- 匹配成功后，把已经验证、规范化的 `got` 写入既有 `X-Channel-Identify-Code` 请求头，再从 URL 移除识别码。下游 `bridgeIdentifyHintFromRequest` 能继续读取可信凭据；不复制未验证的 query，不把 header 与另一 query 混用。
- 只有匹配成功的路径设置 header。无候选时，默认 503；显式关闭鉴权的兼容路径不伪造已验证身份。无效或缺失识别码仍 401 且零下游调用。
- URL 清理只减少下游 URL 暴露，不能改变请求抵达前反代已经记录的日志。测试直接读取 `Request.URL.Query()`，不把 Gin 已缓存的 query 视作新 URL。
- 无数据库、外部 HTTP、模型、设备或消息调用；不改变 Android、租户解析策略、旧 token 或业务响应契约；未提交或部署。
