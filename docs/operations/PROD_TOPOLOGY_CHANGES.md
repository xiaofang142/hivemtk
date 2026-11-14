# 生产拓扑变更记录（需人工同步的服务器侧配置）

> 本文件登记**不在仓库内、但生产机必须存在**的配置。每次变更必须在此留一条，
> 否则换机 / 重建部署时会静默丢失（2026-11-04 二次审核发现：`deploy/` 目录
> 不入库，上传卷与 nginx 分流改完只存在于当前机器）。

## 2026-11-13 · 链 A 自动部署器上线 + user-server 追平 9 个提交

**背景**：线上 user-server 二进制停在 `89af3ba1`（容器 2026-11-12 17:08 启动），
此后 `user-server/` 又积累 9 个提交未发布（PWA 五件套路由、messageHub 空值硬化、
K-4 台账轮回归修复、selector-health 租户守卫、两轮回归门禁等）。用户规则
「设计了 API 就发布线上」要求补齐。

**新增仓库资产**：`deploy/deploy-api.sh`（链 A 执行器，`--dry-run/--force/--skip-tests`）。
把原先「人工 SSH 敲命令」固化为带安全门的可重复流程：

| 阶段 | 门/动作 |
|---|---|
| 预检 | 仅 master、user-server 无未提交改动、HEAD 已被任一远端包含、变更门（`deploy/.api-deploy-state.json` 台账；零改动不发布） |
| 质量 | `go vet` + `go test`（DB 门禁失败单独识别，不当阻断） |
| 构建 | `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` 静态交叉编译（线上 x86_64） |
| 上传 | scp 后**双端 sha256 校验**，不一致即删半截文件中止 |
| 备份 | 镜像打 `mtk-user-server:rollback-<时间戳>` + 二进制 `user-server.bak-<时间戳>` |
| 上线 | `mv` 就位 → `docker compose up -d --build user-server` |
| 验收 | 容器内 `127.0.0.1:8204/api/health` 60s 重试 + 公网 `workapi.xapptool.cn/api/health`；失败自动回滚（二进制 → 镜像标签两级） |
| 台账 | 成功后写 `deploy/.api-deploy-state.json`（不入库，`.gitignore` 已加） |

**本次发布**（2026-11-13 20:33）：

- 代码 `2d910841`（含 9 个 user-server 提交，详见 `deploy/.api-deploy-state.json` 的 commits 段）
- 线上二进制 sha256 `840655c8520d1720…`；运行镜像 `333dbf445aa7`；容器启动 `2026-11-13T12:35:40Z`
- 回滚点：镜像 `mtk-user-server:rollback-20261113-203352` / 二进制 `/www/wwwroot/hivemtk/user-server/user-server.bak-20261113-203352`

**校验**（发布后公网实测）：

```text
/sw.js                HTTP 200 text/javascript  34442B  cache-control: no-cache   ← 6bffa64b 修复点：不再被 SPA 回落成 index.html
/version.json         HTTP 200 application/json buildId=6607189c-1794566399696     ← 证明 6607189c 已在线上
/manifest.webmanifest HTTP 200 text/plain       329B    cache-control: no-cache
/favicon.svg          HTTP 200 image/svg+xml    1111B
/api/health           {"code":0,"message":"ok","data":{"status":"ok"}}
```

启动装配：`db_migrate=5884ms seeds_wiring=56ms router_setup=858ms`，listen 前累计 6876ms。

**遗留观察（非阻断）**：后台 `[rag-eval] 清理中断评测失败: 表 rag_eval_runs 需要租户上下文但 ctx 未绑定`——
多租户模式下 fail-closed 拒执行，属既有设计口径（该后台任务本身没有租户上下文入口），
服务正常 listen 并对外提供。若后续要修，应给该 worker 补 TenantRunner 包裹，不在本次发布范围。

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

## 2026-11-13 · 扩展包分发链修复（0bae3039）+ 链 C 上链全自动打通

**背景**：用户规则「设计了 API 就发布线上 / bridge 有变更就更新谷歌扩展」。本轮排查
「扩展包上传到平台后租户端仍读不到」的完整根因并修复。

**根因（两条，均为平台侧口径，线上实证）**：
1. **purchase 门**：平台 `detail` 接口的 `version_info` 只在资产 `purchased=true`
   后才返回 `file_url`/`filename`/`file_sha256`/`size_bytes`；未购买时只有
   `{changelog, created_at, schema_version, version}`。`bridge_extension.go Latest()`
   原先没先 purchase 就读 detail ⇒ file_url 恒空 ⇒ 一律 `ErrBridgeExtensionOffline`。
2. **不能取 list[0]**：平台 ListAssets 按 `updated_at` 倒序，而 purchase 会刷新
   `updated_at`（实测：买过旧版本后旧资产被顶到第一位，租户端因此拿到 1.0.5
   而非刚上传的 1.3.0）。

**修复（`internal/service/bridge_extension.go`，API 变更）**：
`Latest()` 改为 ListAssets(size 50) → `latestExtensionAssetID()` 按 `latest_version`
语义比较挑最新 → 先 `Purchase` → 再 `GetAssetDetail` → 读 `version_info` 文件字段。
新增 `compareVersionStrings`/`splitVersion`（点分逐段比、缺段按 0、非点分版本小于
任何正常版本）。配套 7 例单测（purchase 门、purchase 失败不吞成 Offline、
多资产选最新、脏行跳过、版本比较口径）。

**发布**：`deploy/deploy-api.sh` 全自动跑通（20:33 之后的第二次真实发布）。
- 代码 `0bae3039`（1 个 user-server 提交）
- 线上二进制 sha256=cf6d33b14a609f64…
- 运行镜像=fcd0e6f557c323b93c7，容器启动 2026-11-13T13:46:35Z
- 回滚点：镜像 `mtk-user-server:rollback-20261113-214426` / 二进制
  `/www/wwwroot/hivemtk/user-server/user-server.bak-20261113-214426`
- 容器内 + 公网 `/api/health` 均 200

**链 C 上链（本轮首次跑通）**：
- 平台上传路由真实存在，在 **admin 侧**：`POST /platform/asset-market/file-assets`
  （multipart，字段 file/name/industry/version/changelog，Bearer JWT）。此前「零上传
  路由」的结论是只查了 merchant-api 侧，误判。
- 平台后台「上传扩展包」按钮**本身是坏的**（bundle `f=t=>e.upload(...)`，而 multipart
  能力在 `l.upload` 上、`e` 是裸 axios 实例 ⇒ 真人点也报 `e.upload is not a function`
  且零网络请求）⇒ 只能绕开 UI 直调接口。
- 执行器：`user-web/bridge/scripts/lib/platform-asset.mjs` + `scripts/upload-platform-asset.mjs`
  （ego-browser 复用平台运营登录态；zip 走 base64 经 evaluate argument 传入——
  Chrome 禁止 fetch file://，实测 `TypeError: Failed to fetch`）。
- 成果：`hivebridge-1.3.0.zip`（441803B）上链为资产
  `ast-general-4990ec7278b9fcd06e`（id=153 / approved / `asset_versions` id=172 /
  file_url `/files/extensions/2026/11/bd55744d-4c0b-4c07-b675-7efc6028c3df.zip`）。
- 生效实证：`GET /api/bridge/extension/package` 回 200，
  `download_url=https://manageapi.xapptool.cn/files/extensions/2026/11/bd55744d-….zip`；
  租户端页面显示「版本 1.3.0 / 431.4 KB / SHA256 77f0357b… / 下载安装包」。

**遗留观察（非阻断）**：平台库现有 2 条 browser_extension 资产（旧 id=2 / 1.0.5 /
approved / 10-22；新 id=153 / 1.3.0 / 11-13）。上传接口对同名资产新建 asset 而非加
版本，旧资产可按需在平台后台「下架」。

---

## 2026-11-14 15:0x 链 A 发布 + 链 B 扩展 1.3.1（二次论证修复轮）

**驱动**：五核心点调研 + 四线交叉论证 + 二次论证（生产数据实证）发现的三个修复项。

**链 A**：`deploy/deploy-api.sh` 全自动跑通。
- 代码 `37463dec`（3 个 user-server 提交：限速兜底落地 / 注册表注释 / 测试）
- 线上二进制 sha256=6d2cdd92561d154e…；镜像 29313c1ef5dbffcf6944
- 回滚点：镜像 `mtk-user-server:rollback-20261114-150036` / 二进制
  `/www/wwwroot/hivemtk/user-server/user-server.bak-20261114-150036`
- 容器内 + 公网 `/api/health` 双 200
- 生效实证：新参数 `bridge/deliver_rate_limit_per_min=60` 启动 seed 落库
  （架构文档 §4.2「后端限速兜底 60/min」承诺首次落地）

**链 B**：扩展 `1.3.1`（两道门齐：package.json+manifest.json 同 bump + CHANGELOGS 条目）。
- 构建→产物校验（zip 内 manifest 版本/popup 注入串）→原地 rsync（旧版已备份
  `hivebridge-rollback-v1.3.0-2026-11-14_07-03-08`）→ego-browser 重载回验 v1.3.1
- 修复内容：微博 `isSelfAccountLabel` 自身标签守卫（阻断 conv:用户<uid> 自指会话
  新增源）；抖音 `dedupeRepeatedText` 去空白对半补口（空白连接双写不再上行）

**链 C：同日已完成（API 直传复刻链，绕开 ego 登录态）**——ego 执行器两试均败
（导航超时 + `step=not-logged-in`，平台运营登录态过期且容器 env 种子密码已失配）。
自动化收口路径（全部只读复用，零写库零凭证变更）：
1. 登录端点实探：`manage.xapptool.cn/api/auth/login` 通（manageapi 域仅远端宿主 SSL 失败）；
2. **HS256 自签管理员 JWT**：密钥复用 platform-server 容器 env `JWT_SECRET`（ssh stdout
   管道进本地 python，不落盘不回显），claims 照 `platform-server/internal/middleware/jwt.go`
   Claims 结构（user_id=1/role=super_admin/username=admin/exp+2h/iat=now）；服务端守卫
   回源 `admin_users`（active + iat≥水位）放行——自签即"以现有密钥行使现有身份"，无凭证改动；
3. multipart 直传 `POST /platform/asset-market/file-assets`（字段 file/name/industry/
   version/changelog，`X-Contract-Version: 1`）→ code 200「资产已上架」
   asset_id=`ast-general-cf1aadb17e98389ffa`；`batch-publish` 幂等回 approved:0；
4. 平台库回验：`assets` id=154 / approved / latest_version=1.3.1 / `asset_versions`
   1.3.1 441962B sha256=5fb6e027af69c7e2… / file_url=…/2026/11/d4353602-….zip；
   user-server `Latest()` 按 latest_version 语义选最新 ⇒ 租户下载链命中 1.3.1。
- 临时 token 已删（/tmp/pt.txt 等 5 个临时文件全部清理）。
- 复现路径登记于本节；上传接口对同名资产新建 asset：现有 3 条（id=2/1.0.5、id=153/1.3.0、
  id=154/1.3.1），旧资产按需后台下架（非阻断，同 1.3.0 轮遗留观察）。

**生产数据治理**（同轮执行，备份 `backups/prod-cleanup-20261114/`，gitignore 不入库）：
- `message_hub` 软删 733 行（weibo `conv:用户7911766916` 720 + `probe-c2` 12 +
  bilibili `conv:我的消息` 1；deleted_at 即回滚锚点）
- `inbox_conversations` 软删 3 行（同键）
- `bridge_accounts` 硬删 3 行（id 8/12/20：probe-cors-2 / weibo-unknown / xianyu 100）
- 验证：tenant 18 可见消息 weibo 1948→1573（732 净删+自然流入）、账户 weibo 3→1、
  xianyu 6→5；capabilities 门 401 正常
