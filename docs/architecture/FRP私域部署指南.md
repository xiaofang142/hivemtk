# HiveMtk 用户端 - FRP 私域部署指南

> 适用对象：需要把部署在内网/NAT/家庭宽带环境的 HiveMtk 用户端，通过公网域名对外提供客服服务的运维/集成商
> 关联文档：[部署方案_用户端.md](部署方案_用户端.md) / [CHAT_WIDGET_EMBED.md](../operations/CHAT_WIDGET_EMBED.md) / [ADR-011-chat-widget-embed.md](adr/ADR-011-chat-widget-embed.md)

---

## 一、为什么需要 FRP

HiveMtk 用户端采用**私域独立部署**：数据库、推理栈、用户数据全部本地化。当客户官网部署在公网（任何访客都能访问的域名），而 HiveMtk 部署在内网（无公网 IP、家庭 NAT、企业防火墙后）时，需要一种方式让公网请求能"穿透"到内网的 `user-server:8204`。

**FRP（Fast Reverse Proxy）** 是这一场景下最轻量的解决方案：

```
访客浏览器 ──HTTPS──> chat.example.com(公网)
                          │
                          ▼
                     云服务器(frps)
                          │
                  frp 隧道（长连接）
                          │
                          ▼
                     本地 frpc ──HTTP──> user-server:8204
```

---

## 二、方案选型

| 方案 | TLS 终结方 | 适用场景 | 复杂度 | 推荐度 |
|------|-----------|---------|--------|--------|
| **A. frps 自终止 TLS** | frps | 不想再装 反向代理层 | ⭐⭐ | ⭐⭐⭐ |
| **B. 反向代理层终止 TLS,frpc=http** | 反向代理层 + frpc | 已有 反向代理层 / 宝塔 | ⭐ | ⭐⭐⭐⭐ |
| **C. Cloudflare Tunnel** | Cloudflare 边缘 | 全球加速 + 不想运维 frps | ⭐ | ⭐⭐ |

> **默认推荐方案 B**：与现有 `hivemtk-platform/scripts/release.sh` 的 `frpc.toml` 模板（方案 A）互补，覆盖更多部署环境。

---

## 三、方案 A：frps 自终止 TLS（轻量级）

### 3.1 云端 frps 部署

#### 3.1.1 安装（二进制方式）

```bash
# 1) 下载 frp v0.70.0（与项目内置版本一致）
wget https://github.com/fatedier/frp/releases/download/v0.70.0/frp_0.70.0_linux_amd64.tar.gz
tar -xzf frp_0.70.0_linux_amd64.tar.gz
cp frp_0.70.0_linux_amd64/frps /usr/local/bin/
mkdir -p /etc/frp

# 2) 写配置
cat > /etc/frp/frps.toml <<'TOML'
bindAddr = "0.0.0.0"
bindPort = 7000
# 与 frpc 共享的认证 token
auth.method = "token"
auth.token = "CHANGE_ME_RANDOM_64_CHARS"
# frps 控制台
webServer.addr = "0.0.0.0"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "CHANGE_ME_DASHBOARD_PASS"
# TLS 强制
transport.tls.force = true
TOML

# 3) systemd 托管
cat > /etc/systemd/system/frps.service <<'UNIT'
[Unit]
Description=FRP Server
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/frps -c /etc/frp/frps.toml
Restart=always
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now frps
systemctl status frps
```

#### 3.1.2 防火墙放行

```bash
# ufw
ufw allow 7000/tcp comment "frps bind"
ufw allow 7500/tcp comment "frps dashboard"
ufw allow 443/tcp comment "https"

# firewalld
firewall-cmd --permanent --add-port=7000/tcp
firewall-cmd --permanent --add-port=7500/tcp
firewall-cmd --permanent --add-port=443/tcp
firewall-cmd --reload
```

#### 3.1.3 DNS 解析

| 域名 | 记录类型 | 值 |
|------|----------|-----|
| `chat.example.com` | A | 云服务器公网 IP |
| `frp.example.com` | A | 云服务器公网 IP（可选,frps 域名） |

### 3.2 TLS 证书（acme.sh 自动签发）

```bash
# 安装 acme.sh
curl https://get.acme.sh | sh -s email=ops@example.com
source ~/.bashrc

# 申请证书（standalone 模式需先停 80 端口）
acme.sh --issue -d chat.example.com --standalone

# 安装到 /etc/frp/certs/
acme.sh --install-cert -d chat.example.com \
  --cert-file /etc/frp/certs/chat.example.com.crt \
  --key-file /etc/frp/certs/chat.example.com.key \
  --fullchain-file /etc/frp/certs/chat.example.com.fullchain.crt \
  --reloadcmd "systemctl reload frps"
```

### 3.3 本地 frpc 部署

frpc 模板可参考项目内 `docs/architecture/FRP私域部署指南.md` 第 3.3 节，或 `hivemtk-platform/scripts/release.sh`（位于平台端仓库，若已 clone 则可复用其 `frp/frpc.toml` 模板；若该路径不可达，说明平台端仓库尚未 clone 或已迁移，请按下方模板自行编写）：

```toml
# frpc.toml（方案 A 完整版）
serverAddr = "frp.example.com"
serverPort = 7000
auth.method = "token"
auth.token = "CHANGE_ME_RANDOM_64_CHARS"  # 与 frps 一致
transport.tls.enable = true

# 把本地 user-server(8204) 暴露为 https://chat.example.com
[[proxies]]
name = "mtk-user-chat"
type = "https"
localIP = "127.0.0.1"
localPort = 8204
customDomains = ["chat.example.com"]
# 证书路径（如果 acme.sh 装在 frps 端,frpc 通过 frps 中转,这里给 frps 的证书路径）
# 实际场景:方案 A 中证书由 frps 加载,frpc 不需要 certFile/keyFile
# 但 v0.70.0 仍要求 https 类型声明证书,使用 frp 内置默认证书 + frps 终止 TLS
transport.useCompression = true
transport.heartbeatInterval = 30
transport.heartbeatTimeout = 90
```

```bash
# 安装 frpc
cp frpc /usr/local/bin/
# 上传 frpc.toml 到 /etc/frp/

# systemd 托管
cat > /etc/systemd/system/frpc.service <<'UNIT'
[Unit]
Description=FRP Client
After=network.target docker.service
Wants=docker.service

[Service]
Type=simple
ExecStart=/usr/local/bin/frpc -c /etc/frp/frpc.toml
Restart=always
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now frpc
systemctl status frpc
journalctl -u frpc -f
```

### 3.4 验证

```bash
# 1) 访客浏览器访问
curl -I https://chat.example.com/health
# 应返回 200 OK

# 2) 浮标脚本可访问
curl -I https://chat.example.com/embed/marketing-chat-widget.iife.js
# 应返回 200 + application/javascript

# 3) 嵌入式聊天窗路由
curl -I https://chat.example.com/chat/embed/default
# 应返回 200(由 user-server 静态托管 + Vue hash 路由)

# 4) WebSocket 测试(用 wscat)
wscat -c "wss://chat.example.com/api/ws/visitor?session_id=test&visitor_id=test&channel_id=default"
# 应能建立连接
```

---

## 四、方案 B：反向代理层终止 TLS,frpc 用 http 代理（推荐）

### 4.1 架构

```
访客 ──HTTPS(443)──> 反向代理层(云端,终止TLS)
                              │
                              ▼
                         frps（仅做隧道,不碰 TLS）
                              │
                       frp 隧道(长连接)
                              │
                              ▼
                         frpc（本地,type=http）
                              │
                              ▼
                         user-server:8204
```

### 4.2 frps 配置（云端）

```toml
# /etc/frp/frps.toml
bindAddr = "0.0.0.0"
bindPort = 7000
auth.method = "token"
auth.token = "CHANGE_ME_RANDOM_64_CHARS"
# 方案 B 不需要 frps 终止 TLS,简化配置
```

### 4.3 静态托管配置（云端,宝塔或原生）

### 4.4 frpc 配置（本地）

```toml
# /etc/frp/frpc.toml
serverAddr = "frp.example.com"
serverPort = 7000
auth.method = "token"
auth.token = "CHANGE_ME_RANDOM_64_CHARS"  # 与 frps 一致
transport.tls.enable = true

# 关键:type=http + 不带 certFile(让 反向代理层终止 TLS)
[[proxies]]
name = "mtk-user-chat"
type = "http"
localIP = "127.0.0.1"
localPort = 8204
customDomains = ["chat.example.com"]
# WebSocket 必加,否则握手失败
transport.useCompression = true
transport.heartbeatInterval = 30
transport.heartbeatTimeout = 90
```

> 端口 8204 → 8205:这里把本地 8204 通过 frp 暴露到云端 8205(端口任意,只要不被占用),反向代理层 再把 443 反代到 8205。这种"双跳"避免 frpc 与 反向代理层 端口冲突,也便于同机多实例(每个应用/客户用独立 8xxx 端口避免冲突)。

### 4.5 验证

```bash
# 1) 云端直接 curl(走 反向代理层)
curl -I https://chat.example.com/health
# 200 OK

# 2) 绕过 反向代理层 直连 frpc(应该 404,因为 type=http 期待完整 HTTP 请求)
curl -I http://127.0.0.1:8205/health
# 200 OK

# 3) WebSocket
wscat -c "wss://chat.example.com/api/ws/visitor?session_id=test&visitor_id=test&channel_id=default"
# 连接成功 + 收到 ping/pong
```

---

## 五、方案 C：Cloudflare Tunnel（零运维）

适合不想自己维护 frps 的场景：

### 5.1 安装 cloudflared（本地）

```bash
curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 -o /usr/local/bin/cloudflared
chmod +x /usr/local/bin/cloudflared
```

### 5.2 登录 + 建隧道

```bash
cloudflared tunnel login  # 浏览器授权
cloudflared tunnel create mtk-user-chat
```

### 5.3 配置

```yaml
# ~/.cloudflared/config.yml
tunnel: mtk-user-chat
credentials-file: /root/.cloudflared/<TUNNEL_ID>.json

ingress:
  - hostname: chat.example.com
    service: http://127.0.0.1:8204
  - service: http_status:404
```

### 5.4 DNS + 运行

```bash
cloudflared tunnel route dns mtk-user-chat chat.example.com
cloudflared tunnel run mtk-user-chat
```

### 5.5 systemd 托管

```ini
# /etc/systemd/system/cloudflared.service
[Unit]
Description=Cloudflare Tunnel
After=network.target

[Service]
Type=notify
ExecStart=/usr/local/bin/cloudflared tunnel --no-autoupdate run mtk-user-chat
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

> Cloudflare Tunnel 自动 HTTPS,但需要在 Cloudflare DNS 添加站点。免费版足够。

---

## 六、WebSocket 穿透关键参数

WebSocket 长连接通过 FRP 时容易断,以下参数**必加**：

```toml
# frpc 端（所有方案通用）
transport.heartbeatInterval = 30    # 30s 一次 ping
transport.heartbeatTimeout = 90     # 90s 无响应判定失联
transport.tcpKeepalive = 60         # TCP keepalive

### 反向代理层 端（方案 B 必加）
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
proxy_read_timeout 300s;            # 大于 heartbeatTimeout
```

---

## 七、与 docker-compose / host 模式的集成（可选）

> **架构前提**：自 2026-08-17 改造后，user-server 不再以容器方式运行（详见 [部署方案_用户端.md §三](部署方案_用户端.md)），其数据层 PG/Redis 仍由根目录 `docker-compose.yml` 托管（`mtk-postgres`、`mtk-redis` 两个服务），共享网络 `mtk-user-network`。frpc 仅需**与 user-server 同主机**即可访问 `127.0.0.1:8204`。

### 7.1 容器化 frpc（推荐：frpc 仍放 Docker）

```yaml
# docker-compose.yml 追加（与 mtk-postgres / mtk-redis 并列）
services:
  frpc:
    image: snowdreamtech/frpc:0.70.0
    container_name: mtk-frpc
    restart: unless-stopped
    volumes:
      - ./frp/frpc.toml:/etc/frp/frpc.toml:ro
    # 共享 PG/Redis 所在网络，便于在容器内解析 mtk-postgres 等服务名
    networks:
      - mtk-user-network
    # 注意：user-server 不再是 compose 服务，因此这里不再写 depends_on: user-server
    # frpc 通过 host gateway (默认 172.17.0.1) 反向访问 host 上的 127.0.0.1:8204：
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

**注意**：frpc 必须能与 user-server 通信，且能访问 `frps` 的公网 7000 端口。docker 网络默认能访问公网，无需特殊配置。

### 7.2 frpc 跑在宿主机（更简单：直接 systemd 托管）

如果你不想为 frpc 单开一个容器，直接在宿主机跑 `frpc -c /etc/frp/frpc.toml`，并按 §3.3 配置 systemd unit 即可，**无需**写 docker-compose 集成段。后续若 `mtk-postgres` 容器需要让 frpc 访问，把 PG 端口 `8202` 映射到 `127.0.0.1:8202` 即可（已在根 `docker-compose.yml` 中按 `127.0.0.1:8202:5432` 绑定，宿主机可直接访问）。

---

## 八、健康检查

### 8.1 frpc 进程守护

```bash
# systemd 已经 Restart=always
# 但建议加 watchdog
cat > /usr/local/bin/frpc-watchdog.sh <<'SH'
#!/bin/bash
if ! pgrep -f "frpc -c" >/dev/null; then
  echo "[$(date)] frpc dead, restarting" >> /var/log/frpc-watchdog.log
  systemctl restart frpc
fi
SH
chmod +x /usr/local/bin/frpc-watchdog.sh

# crontab -e
*/5 * * * * /usr/local/bin/frpc-watchdog.sh
```

### 8.2 端到端探活

```bash
# 每分钟 curl 一次,失败 3 次记录到日志
cat > /usr/local/bin/mtk-healthcheck.sh <<'SH'
#!/bin/bash
for i in 1 2 3; do
  if curl -fsS https://chat.example.com/health >/dev/null 2>&1; then
    exit 0
  fi
  sleep 5
done
# 全部失败: 私域部署无外部告警通道, 仅写入应用层日志
echo "[$(date)] ERROR: HiveMtk user-server 健康检查连续 3 次失败" >> /var/log/hivemtk-healthcheck.log
exit 1
SH
chmod +x /usr/local/bin/mtk-healthcheck.sh

# crontab -e
* * * * * /usr/local/bin/mtk-healthcheck.sh
```

---

## 九、安全要点

| 项 | 建议 |
|----|------|
| **auth.token** | `openssl rand -hex 32`,与 frps 严格一致 |
| **TLS** | 强制 TLS 1.2+；禁用 SSLv3/TLS 1.0/1.1 |
| **dashboard** | frps 控制台加白名单 IP 或改非默认端口 |
| **fail2ban** | frps 7000 端口接 fail2ban,防暴力枚举 |
| **真实 IP 透传** | 反向代理层 `X-Forwarded-For` + user-server `CORS_ALLOW_ORIGINS_USER` 校验 |
| **Webhook secret** | 若用 frpc webhook 做动态域名,加 secret 校验 |
| **定期轮换** | 90 天轮换一次 `auth.token` + TLS 证书 |

---

## 十、常见问题排查

### Q1: 浏览器打开 `https://chat.example.com` 显示 502

- 检查 frpc 状态：`systemctl status frpc`
- 检查 frpc 日志：`journalctl -u frpc -n 50`
- 检查 user-server 状态：`docker compose ps user-server`
- 检查 user-server 8204 端口：`curl http://127.0.0.1:8204/health`

### Q2: 能打开页面但 WebSocket 一直 reconnecting

- 反向代理层 缺 `proxy_set_header Upgrade` 配置（方案 B）
- frpc 缺 `transport.heartbeatInterval`
- 浏览器 DevTools Network → WS 帧，查看是否收到 ping/pong

### Q3: postMessage 跨域失败

- 嵌入的 iframe 域名与父页 postMessage origin 不一致
- 解决：父页 SDK 的 `data-api-base-url` 必须与 iframe 实际加载的 origin 完全一致
- 调试：`window.addEventListener('message', e => console.log(e.origin, e.data))`

### Q4: TLS 证书路径错误

- frps 类型 https 必填 certFile/keyFile（方案 A）
- 路径必须是 frps 容器/进程能读到的绝对路径
- acme.sh 申请后默认路径：`/root/.acme.sh/chat.example.com/`

### Q5: frps 7000 端口被运营商封

- 部分 ISP 屏蔽 7000，改用 443/80 等常用端口
- frps bindPort 改 443 + 让出 443 端口 443 给 frps
- 或换方案 C（Cloudflare Tunnel）

### Q6: 多客户共享一个 frps

- 每个客户独立 `[[proxies]]` 段 + 不同 `name` + 不同 `customDomains`
- frps 资源足够（每隧道 < 1MB 内存）
- user-server 的 `CORS_ALLOW_ORIGINS_USER` 放行所有客户域名

---

## 十一、回滚预案

| 故障 | 回滚方案 |
|------|----------|
| frps 故障 | 切到备用 frps（不同云厂商）；客户官网先回退到 `data-api-base-url` 直连 user-server 公网 IP |
| frpc 故障 | 本地 fallback：直接用公网 IP + 反代；短期可接受无 TLS |
| 证书过期 | 提前 30 天 acme.sh 续期；监控 `/etc/frp/certs/` 过期时间 |
| 性能瓶颈 | frps 加机器（横向扩展）；CDN 化（静态资源走 CDN） |

---

## 十二、参考资源

- FRP 官方文档：https://gofrp.org
- acme.sh：https://github.com/acme-sh/acme.sh
- Cloudflare Tunnel：https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/
- 项目 frpc 模板：`hivemtk-platform/scripts/release.sh` → `frp/frpc.toml`（位于平台端仓库，若已 clone 可复用；若不可达，按本指南 §3.3 模板自行编写）

---

## 十三、参考架构：自建 frps + 同机反向代理（混合模式）

> 本节是一套**可照抄的参考架构**，不是任何在跑的部署：早期版本这里写的是作者自有的到期云服务器
> （域名与口令均已从文档中清除，见 §13.4/§13.5 的 `CHANGE_ME`），那台机器已于 2026-09 下线不续费。
> 域名一律用 `*.hivemtk.example.com` 占位，落地时换成你自己的。
>
> **2026-09-03 踩坑记录**：之前按"独立 Chat 整站穿透"理解架构，错误地把前端整站域名 `user.hivemtk.example.com`
> 都走 frp，实际上**前端静态包在服务器 反向代理层 上**，只有 API 路径才走 frp。此节记录正确架构和必守铁律，避免再犯。

### 13.1 架构图

```
                       云端服务器 (<你的-frps-主机-公网IP>)
                              ┌─────────────────────────────┐
                              │                             │
  ┌────────────────────┐      │   ┌─────────────────┐       │
  │  用户浏览器         │──HTTPS──>│  反向代理层    │       │
  └────────────────────┘      │   │  443 ssl        │       │
                              │   └────────┬────────┘       │
                              │            │                │
                              │   ┌────────┴────────┐       │
                              │   │ 路径分流         │       │
                              │   ├─────────────────┤       │
                              │   │ /               │       │
                              │   │ /assets/*       │       │
                              │   │ /favicon.svg    │       │
                              │   │                 │       │
                              │   │  → root dist/   │       │
                              │   │  (反向代理层 直接读)  │       │
                              │   └─────────────────┘       │
                              │            │                │
                              │   ┌────────┴────────┐       │
                              │   │ /api/*           │       │
                              │   │ /chat/embed      │       │
                              │   │                  │       │
                              │   │  proxy_pass      │       │
                              │   │  127.0.0.1:8280  │       │
                              │   │  Host 改写       │       │
                              │   │  ↓               │       │
                              │   │  frps vhostHTTP  │       │
                              │   │  (同机 frps)     │       │
                              │   └────────┬─────────┘       │
                              │            │                │
                              │            │ frp 隧道       │
                              │            │ (长连接)        │
                              └────────────┼─────────────────┘
                                           │
                                           ▼
                        ┌──────────────────────────────┐
                        │   本地开发机                  │
                        │   ┌────────────────────────┐ │
                        │   │  frpc → 127.0.0.1:7000  │ │
                        │   └────────┬───────────────┘ │
                        │            │                  │
                        │            ▼                  │
                        │   ┌────────────────────────┐ │
                        │   │ user-server :8204      │ │
                        │   │ (Go, 含 LLM 推理)       │ │
                        │   └────────────────────────┘ │
                        │            │                  │
                        │            ▼                  │
                        │   ┌──────────┐  ┌─────────┐  │
                        │   │ PostgreSQL│  │ Redis   │  │
                        │   └──────────┘  └─────────┘  │
                        └──────────────────────────────┘
```

### 13.2 域名 / frps customDomain / 本地端口 对照表

| 公网域名 | frpc customDomain | frps vhost 匹配后转到 | 本地服务 |
|----------|-------------------|-----------------------|----------|
| `user-api.hivemtk.example.com` | `user-api.hivemtk.example.com` | 本地 :8204 | Go user-server |
| `user.hivemtk.example.com/api/*` | (无独立域名, **反向代理层 里 Host 改写**到上一行) | 同 `user-api.hivemtk.example.com` | Go user-server |
| `user.hivemtk.example.com/chat/embed` | `user.hivemtk.example.com` | 本地 :8204 | Go user-server (embed 路由) |

### 13.3 反向代理层 关键配置（避免踩坑的完整模板）

见仓内 `docs/operations/reverse-proxy/nginx.conf.template`——它已把本节所有铁律落成可抄的 location 块
（SSE 关缓冲、WS 透传 Upgrade、frps vhost 显式改写 Host）。本节不再重复贴一份，避免两处漂移。

### 13.4 frpc 配置（本地开发机）

```toml
serverAddr = "<你的-frps-主机-公网IP>"   # 或域名
serverPort = 7000
auth.token = "CHANGE_ME_RANDOM_64_CHARS"   # 与 frps 一致；别把真 token 提交进仓库
transport.tls.enable = true

# 主 API 隧道 (user-api 整站 + user.example.com/api/* 的 Host 改写都走这里)
[[proxies]]
name = "mtk-user-chat"
type = "http"
localIP = "127.0.0.1"
localPort = 8204
customDomains = ["user-api.hivemtk.example.com"]
transport.useCompression = true

# /chat/embed 隧道 (可选, 不需要热更新可删)
[[proxies]]
name = "mtk-user-web-embed"
type = "http"
localIP = "127.0.0.1"
localPort = 8204
customDomains = ["user.hivemtk.example.com"]
transport.useCompression = true
```

### 13.5 frps 配置（云端, 同机 反向代理层）

```toml
# /www/wwwroot/frp/frps.toml
bindPort = 7000                        # 控制连接端口 (frpc 连这个)
auth.token = "CHANGE_ME_RANDOM_64_CHARS"   # 与 frpc 同一个随机值
vhostHTTPPort = 8280                   # ← 关键! 同源托管这个端口
transport.tcpMux = true
transport.maxPoolCount = 10
webServer.addr = "0.0.0.0"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "CHANGE_ME"           # 7500 后台口令；曾在本文件里明文写过一条真口令，已清除，请轮换
# 注意: 反向代理层 占了 80/443, 所以 frps 不直接监听这些端口
# frps 只提供 8280 vhost, 由 反向代理层 转发过来
```

### 13.6 排错 Checklist（按顺序执行）

```
场景: https://user.hivemtk.example.com/api/health 返回 404 / 502 / 连接超时

[Step 1] 本地 Go 服务是否在跑?
         curl -sS http://127.0.0.1:8204/health
         → 期望: {"code":0,"status":"ok"}
         → 挂了? 启动: GIN_MODE=debug MASTER_KEY=... ./mtk-serve

[Step 2] frpc 是否在跑 + 是否已注册 customDomain?
         pgrep -af frpc
         cat /tmp/frpc.log | tail -20
         → 期望: "start proxy success" 且无 "already exists" 持续刷屏
         → 看 frps 端: curl -u admin:$PASS http://frps:7500/api/v1/proxy/http

[Step 3] frps vhost 端口是多少?
         ssh root@<你的-frps-主机> 'ss -tlnp | grep frps'
         → 期望: LISTEN *:8280 (不是 7000!)
         → 如果 frps 没监听 vhostHTTPPort: 检查 frps.toml + systemctl restart frps

[Step 4] frps 能否直接匹配 Host? (绕过 反向代理层 验证)
         curl -sS -H "Host: user-api.hivemtk.example.com" http://127.0.0.1:8280/api/health
         → 本地 frps 上执行 (或 ssh 进服务器后测)
         → 返回 200 → frp 链路 OK, 问题在 反向代理层 层
         → 返回 404 → frpc customDomain 没注册上 (Step 2 检查)
         → 返回 502 → frpc customDomain 存在但 local service 挂了 (Step 1 检查)

[Step 5] 反向代理层 Host header 是否改写了? (最常见的坑!)
         → grep "proxy_set_header Host"
         → 期望: proxy_set_header Host  user-api.hivemtk.example.com;
         → 如果是 $host 或前端那个域名 → 改过来!

[Step 6] 反向代理层 proxy_pass 端口对不对?
         → 同机 frps 用 127.0.0.1:8280 (内网回环)
         → 跨机 frps 用 IP:8280 (公网 IP)
         → 错写成 7000? 那是 frp 控制端口, 不是 vhost HTTP 端口!

[Step 7] 浏览器能通但 API 404 → 前端 API baseURL 写错域名
         curl -sS https://user.hivemtk.example.com/ | grep -o 'api.*base.*url'
         检查 Vite src/core/constants.js DEFAULT_USER_SERVER
```

### 13.7 本次踩坑记录

| 时间 | 错误行为 | 根因 | 正确做法 |
|------|---------|------|---------|
| 2026-09-03 10:00 | 把前端域名整站反代 frps | 以为前端也走 frp 热更新 | 前端静态包托管在服务器, 只有 `/api/` 走 frp |
| 2026-09-03 10:00 | frpc.toml 加了 `mtk-user-web` proxy | 对应上条错误理解 | 删除, 前端 反向代理层 直接读 dist/ |
| 2026-09-03 10:00 | Vite 加 `allowedHosts: true` 为了 frp 反代 | 前端不走 frp, 这条不需要 | 回滚 |
| 2026-09-03 10:05 | 反向代理层 `location /api/` 透传 `$host` | 忘了 frps 按 Host 匹配 customDomain | 显式 `proxy_set_header Host <API 侧 customDomain>` |
| 2026-09-03 10:15 | frpc 被杀后残留进程冲突 | 用 kill -9 PID 但不知道 root 用户也在跑 | 先看 `ps aux \| grep frpc` 找出所有用户的进程 |
| 2026-09-03 10:15 | frpc.toml 文件 `operation not permitted` | macOS 扩展属性 `com.apple.quarantine` | `xattr -cr frpc.toml` |
| 2026-09-03 10:20 | Go 服务 `MASTER_KEY missing` 退出 | 没 export GIN_MODE=debug + MASTER_KEY | `GIN_MODE=debug MASTER_KEY=<32字节+> ./mtk-serve` |
| 2026-09-03 10:20 | 以为 frps 监听 80/443 | 反向代理层 占了 80/443, frps 监听 `vhostHTTPPort=8280` | 反向代理层 `proxy_pass http://127.0.0.1:8280` |

### 13.8 一句话口诀

> **前端在服务器, API 走 frp; Host 必改写, 端口是 8280。**
