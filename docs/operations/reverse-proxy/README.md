# 反代配置模板（v3 审计 P1-A1）

> **关键约束**：SSE 端点（`/api/sse/*`）和 WebSocket（`/api/ws/*`）对反代有特殊要求。

## 必读：HTTP/2 与 SSE 的张力

SSE（Server-Sent Events）基于 HTTP 长连接，要求：
1. **HTTP/1.1 持久连接**（HTTP/2 多路复用可能切断长连接）
2. **proxy_buffering off**（nginx 默认开启缓冲会导致 SSE 延迟）
3. **flush_interval -1**（Caddy 等同设置）

## 模板清单

| 反代 | 模板 | HTTP/2 关闭方式 |
|---|---|---|
| nginx | `nginx.conf.template` | `http2 off;`（per-server） |
| Caddy | `caddy.Caddyfile.template` | `transport http { versions h1 }` |
| Traefik | `traefik.yml.template` | `entryPoints.websecure.http2.enabled: false` |
| FRP | `frpc.toml.template` | 默认 HTTP/1.1，无需额外配置 |

## 验证 SSE 正常

```bash
# 1. 启动反代
# 2. 浏览器打开 /api/sse/dashboard
# 3. 应当每秒看到 : heartbeat 推送

# 命令行验证
curl -N http://your-domain.com/api/sse/dashboard?topic=health
```

## 升级指南

如果你的反代配置来自老版本，升级时必须：
1. 显式加 `http2 off`（或等价配置）
2. SSE 路径加 `proxy_buffering off`
3. WebSocket 路径加 Upgrade 头
