# 生产拓扑变更记录（需人工同步的服务器侧配置）

> 本文件登记**不在仓库内、但生产机必须存在**的配置。每次变更必须在此留一条，
> 否则换机 / 重建部署时会静默丢失（2026-11-04 二次审核发现：`deploy/` 目录
> 不入库，上传卷与 nginx 分流改完只存在于当前机器）。

## 2026-11-05 · 通用短链 `/s/` 反代

**问题**：卡片/分享短链生成的是同源相对路径 `/s/<code>`，后端实际路由是
`/api/s/:code`；`work.xapptool.cn` 没有 `/s/` 反代，`workapi.xapptool.cn`
的 `location /` 又会把未知路径回落成前端 SPA ⇒ 短链打开是登录页/首页，不是目标页。

**线上改动**（`work.xapptool.cn.conf` 与 `workapi.xapptool.cn.conf` 各加一段）：

```nginx
# /s/<code> → 后端 /api/s/<code>
location ^~ /s/ {
    proxy_pass http://127.0.0.1:8204/api/s/;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_read_timeout 120s;
}
```

**校验**：
```bash
curl -sS -o /dev/null -D - https://work.xapptool.cn/s/eIZ5MV | head -1    # 302
curl -sS -o /dev/null -D - https://workapi.xapptool.cn/s/eIZ5MV | head -1 # 302
```

**代码配套**：`ShareShortLink` 生成绝对短链时改用 `PUBLIC_BASE_URL`
（`config.GetPublicBaseURL()`），未配置才回落 `localhost:8204`；卡片响应仍给
相对 `/s/<code>`，由上述 nginx 统一承接。

## 2026-11-04 · 资产封面/扩展包静态文件分流

**问题**：`assets.cover_url` 存的是相对路径 `/files/covers/...`，前端 `<img>`
落在 `work.xapptool.cn` 域；该域 `/files/` 反代到 user-server(8204)（素材库），
而封面文件在 platform-server 的 `/app/uploads`（8205）→ 27 张封面全裂。

**线上改动**（`/www/server/panel/vhost/nginx/work.xapptool.cn.conf`）：

```nginx
# 资产封面 / 扩展安装包在平台端（8205）；其余 /files/ 仍走 user-server（8204）
location ^~ /files/covers/ {
    proxy_pass http://127.0.0.1:8205;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
location ^~ /files/extensions/ {
    proxy_pass http://127.0.0.1:8205;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

**校验**：`curl -o /dev/null -w '%{http_code}' https://work.xapptool.cn/files/covers/...`
应为 200；`/files/<素材库路径>` 仍由 8204 服务（404 表示文件不存在，不是路由问题）。

## 2026-11-04 · uploads 持久卷（防容器重建丢文件）

**问题**：platform-server / user-server 容器的 `/app/uploads` 未挂宿主机卷，
`docker compose up --build` 重建即丢全部封面与扩展包（2026-09 已发生过一次，
2026-11 二次回归再次复现）。

**线上改动**（`/www/wwwroot/hivemtk/platform-server/docker-compose.yml` 与
`.../user-server/docker-compose.yml`）：

```yaml
    volumes:
      - ./uploads:/app/uploads     # platform-server
      # user-server 追加到既有 state/user-web-dist 同一块
```

**换机迁移清单**：
1. 建目录 `platform-server/uploads`、`user-server/uploads`；
2. 从旧机 `rsync` 整个 uploads 目录；
3. `docker compose up -d` 后确认 `docker inspect <container>` 的 Mounts 含该绑定。

## 2026-11-04 · 平台端 LLM 代理路径免全站 IP 限流

已在代码内（`platform-server/internal/middleware/ratelimit.go` 的
`rateLimitExemptPrefixes`），无需服务器侧配置；此处仅登记口径：`/llm-api/v1/`
已有 HMAC + nonce + 钱包扣费三道闸，不再叠 IP 桶（否则一次 AI 编排十余次
并发调用必被 20rps 全站桶打爆）。
