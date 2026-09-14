# 私域部署强制要求（Private Network Required）

> **v3 审计结论 [P0-S1]**：user-server 的鉴权设计是"AppKey 软解析 + 私域部署基线"，**任何公网暴露 = 90% 业务 API 无鉴权 = 数据裸奔**。

## 强制要求

1. **禁止直接暴露 user-server (8204) 到公网**
2. **必须通过以下任一方式**：
   - 内网访问（仅本机/同 VPC）
   - FRP 私域穿透（[FRP 私域部署指南](../../../docs/architecture/FRP私域部署指南.md)）
   - nginx / Caddy 反代 + 强鉴权 + IP 白名单（[反代配置模板](../../../docs/operations/reverse-proxy/README.md)）

## 启动护栏

`v3.0+` 起，启动时自动校验（实现：`internal/security/network_exposure_guard.go`，接入：`cmd/api/main.go`）：

```bash
# .env 中必须显式声明
PUBLIC_BASE_URL=http://10.0.0.5:8204          # 内网 IP
REQUIRE_PRIVATE_NETWORK=true                  # 强制私域

# 错配到公网 IP 时启动失败
# PUBLIC_BASE_URL=http://203.0.113.5:8204  ← 启动报错
```

## 显式关闭护栏（不推荐）

```bash
REQUIRE_PRIVATE_NETWORK=false
```

关闭时**必须同时满足**：
1. AppKey 已启用强鉴权（非软解析）
2. 已加 IP 白名单
3. 已开启审计日志 + 异常登录告警

## 配套：LICENSE 合规

公网对外提供服务还涉及 AGPL-3.0 第 13 条开源义务，参见
[LICENSE 合规自检](../../../docs/operations/LICENSE_COMPLIANCE.md)。

## FAQ

**Q: 本地开发可以关闭护栏吗？**
A: 是。`.env.development` 设 `REQUIRE_PRIVATE_NETWORK=false`。
