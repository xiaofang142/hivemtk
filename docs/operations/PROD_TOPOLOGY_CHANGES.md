# 生产拓扑变更记录（需人工同步的服务器侧配置）

> 本文件登记**不在仓库内、但生产机必须存在**的配置。每次变更必须在此留一条，
> 否则换机 / 重建部署时会静默丢失（2026-11-04 二次审核发现：`deploy/` 目录
> 不入库，上传卷与 nginx 分流改完只存在于当前机器）。

## 2026-11-05 · 租户 1 平台 LLM 钱包与高优路由兜底

**问题**：线上租户 1（`tenant_code=local-xw0hgtdo`）能查到平台下发的
`platform-*` provider，但 `platform_db.llm_wallets` 没有该租户行 ⇒ 代理侧余额
不足，内容生成静默回落 `heuristic_fallback`，GEO FAQ 也降级成检索摘要。

**线上数据改动**（`mtk-platform-postgres/platform_db`）：

```sql
-- 按新开户赠送口径给租户 1 发放 2000 万输入 / 400 万输出 token
INSERT INTO llm_wallets (merchant_key, tenant_code, input_tokens_remaining, output_tokens_remaining,
                         total_recharged_input, total_recharged_output, ...)
VALUES (:merchant_key, 'local-xw0hgtdo', 20000000, 4000000, 20000000, 4000000, ...);
INSERT INTO llm_wallet_txns (..., reason, ref_type, note)
VALUES (..., 'gift', 'signup', '线上租户1平台LLM开户赠送 ...');
```

**线上配置改动**（`user_db.llm_routing_rules`，`high_quality` 场景）：

```sql
UPDATE llm_routing_rules
SET route_json = jsonb_set(route_json::jsonb, '{min_quality}', '0.94')::text
WHERE tenant_id = 1 AND scenario = 'high_quality';
```

原因：`platform-sensenova` 偶发上游 429（`inference exceeds tpm/rpm limit`），
而备选 `platform-dashscope` 质量分 0.94 被原阈值 0.95 挡掉，导致 high_quality
场景无可用 provider。阈值降到 0.94 后 dashscope 可作为真实兜底。

**校验**：GEO `content/generate` 返回 `model=deepseek-v4-flash` 且平台钱包有
`consume` 流水；`geo/kb/ask` 在知识库有匹配文档时返回
`provider=platform-sensenova`，不再走 `heuristic_fallback`。

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
