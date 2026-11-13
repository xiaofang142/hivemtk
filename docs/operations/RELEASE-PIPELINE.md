# HiveMtk 发布链（API 线上 / 蜂桥扩展 / 平台上链）
> 权威口径：本文件是「代码改完了，怎么让它到用户手里」的唯一入口。
> 基线：2026-11-13（考古 `.github/workflows/`、`deploy/`、`docs/operations/PROD_TOPOLOGY_CHANGES.md`、
> `user-web/bridge/scripts/` 与历史提交后重建）。
>
> 配套执行器：`user-web/bridge/scripts/publish.mjs`（链 B 一键完成）。

## 0. 一页结论

改动落在哪里，就走对应的链，**三条链互不替代**：

| 改动范围 | 链路 | 现状 |
|---|---|---|
| `user-server/**`（Go API） | **链 A：发布线上** | ✅ 全自动：`deploy/deploy-api.sh`（变更驱动 + 双健康检查 + 自动回滚） |
| `user-web/bridge/**`（谷歌扩展） | **链 B：本机构建 + 装进 Chrome + 重载** | ✅ 全自动：`npm run publish -- --reload` |
| 上面任一条合并后 | **链 C：安装包上链平台** | ⛔ 无法自动：平台端**零资产上传 HTTP 路由**（2026-11-13 二进制 strings 全量提取实证），只能平台运营后台上传 |

铁律（两条，来自用户 2026-11-13 指令）：
1. **设计了 API 就发布线上**；
2. **设计了 bridge 就更新谷歌扩展**。

## 1. 链 A：API 发布线上（2026-11-13 起全自动）

### 1.1 现状

- **CI 仍无部署 job**。`.github/workflows/release.yml` 只在 tag 推送时打 zip + SBOM +
  GitHub Release 附件；镜像发行链已删（注释：恢复镜像发行时先补 Dockerfile）。
- 线上形态（2026-11-13 实测 `mtk-prod`，勿臆测）：**Docker**，非 systemd/helm。
  服务器 `/www/wwwroot/hivemtk/user-server/`（非 git 仓）+ `docker-compose.yml`，
  容器 `mtk-user-server`（image `mtk-user-server:latest`，`127.0.0.1:8204→8204`），
  同 compose 内 `mtk-postgres`（pgvector/pg15，容器内 8202）、`mtk-redis`（8203），
  共享外部网络 `hivemtk-cloud`。nginx：`workapi.xapptool.cn` → 全量 8204；
  `work.xapptool.cn` 静态 SPA + `/api/`、`/ws/`、`/files/`、`/share/`、`/s/` 等反代 8204，
  `^~ /files/extensions/`、`^~ /files/covers/` 反代 8205（平台端）。
- 历史证据：212505db（O 轮）记录「已部署线上 buildId=83832743-1794542399855」——
  即代码 push 后**人工**在服务器拉取构建部署。
- 服务器侧不同步的配置（nginx 片段、compose environment 等）登记在
  `docs/operations/PROD_TOPOLOGY_CHANGES.md`，改线上拓扑必须同步更新该台账。

### 1.2 判定「是否需要发布」

- 变更门由 `deploy/deploy-api.sh` 自动判：以 `deploy/.api-deploy-state.json` 的
  `deployed_sha` 为基线，`git log <基线>..HEAD -- user-server/` 为空 ⇒ **不发布**
  （零改动不该无脑重发）。台账缺失时按服务器二进制 mtime 收敛基线并强制 `--force`。
- 可观测佐证：`https://workapi.xapptool.cn/version.json` 的 `buildId` 只反映**前端**
  （user-web）构建，**不能**反映 Go 后端版本；后端无公开版本接口
  （`GET /api/system/info` 匿名只回 `platform_enabled`）。
  ⇒ 后端线上版本只能以 `PROD_TOPOLOGY_CHANGES.md` / `deploy/.api-deploy-state.json` 台账为准。

### 1.3 执行（一条命令）

```bash
deploy/deploy-api.sh              # 变更驱动：检测到未发布提交才部署
deploy/deploy-api.sh --dry-run    # 只预检+编译，不碰线上
deploy/deploy-api.sh --force      # 跳过变更门（首次/台账失效时）
deploy/deploy-api.sh --skip-tests # 跳过 go test
```

流程：预检（master / 无未提交 / 已 push / 变更门）→ `go vet` + `go test`
（DB 门禁失败单独识别）→ `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` 交叉编译 →
scp + **双端 sha256 校验** → 镜像打 `rollback-<时间戳>` 标签 + 二进制 `.bak` 备份 →
`docker compose up -d --build user-server` → 容器内与公网双健康检查 →
失败**自动回滚**（二进制 → 镜像标签两级）→ 写台账。

前置：本机 `~/.ssh/config` 有 `mtk-prod` 且 BatchMode 免密可用（已具备）。

发布后必须：①更新 `PROD_TOPOLOGY_CHANGES.md` 台账（脚本会提示）；②如涉及迁移，
按仓内迁移规程执行（db_migrate 在容器启动时自动跑，耗时见启动日志汇总）。

## 2. 链 B：蜂桥扩展（全自动）

### 2.1 为什么不是 Chrome Web Store

扩展**不上架**。平台运营把安装包作为 `browser_extension` 类型资产上传平台
（链 C），租户在用户端网页「桥接扩展安装包」页
（`user-web/src/views/bridge/ExtensionPackage.vue` → `GET /api/bridge/extension/package`，
路由 `/bridge/extension`）下载 zip，自行「加载已解压的扩展程序」。
下载通道服务：`user-server/internal/service/bridge_extension.go`。

### 2.2 一条命令（本机）

```bash
cd hivemtk/user-web/bridge
BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn \
HIVEBRIDGE_LOAD_DIR=/Users/xiaofang/Documents/tool/hivebridge-1.0.2 \
npm run publish -- --reload
```

`scripts/publish.mjs` 做四件事：

1. **版本一致性预检**：`package.json` 与 `manifest.json` 必须同版本（Chrome 读 manifest、
   zip 名读 package，不一致则 Chrome 里看到的版本和包名对不上）。
2. **发行口径门**：不带 `--self-hosted` 就必须有 `BRIDGE_ONLINE_SERVER`，
   否则拒绝构建——防止把 localhost 包当线上包发出去（1.0.4 历史教训）。
3. **构建 + 打包 + 产物校验**：调 `npm run release`（自带「版本一致 + CHANGELOGS 有条目」
   前置门），再校验 dist/zip 内 manifest 版本与 popup.js 注入串。
4. **安装 + 重载**：整份备份旧加载目录（保留最近 5 份回滚副本）→ `rsync -a --delete`
   → 调 ego-browser 打开 `chrome://extensions`、填搜索框过滤到唯一卡片、点「重新加载」，
   并以快照里的版本号回验。

选项：`--skip-build`（只安装现有 dist）、`--dry-run`（只打印）、`--self-hosted`（自部署产物）。

### 2.2b 租户侧安装链（下载 → 解压 → 安装 → 重载）

```bash
cd hivemtk/user-web/bridge
node scripts/install-extension.mjs --zip release/hivebridge-1.3.0.zip   # 本地 zip
node scripts/install-extension.mjs --dir /path/to/unpacked              # 已解压目录
node scripts/install-extension.mjs --download                           # 从用户端页面下载
```

`scripts/install-extension.mjs` 与 publish.mjs 共用 `scripts/lib/ego-browser.mjs`
的 `reloadExtension()`。三种取包来源互斥（`--zip` / `--dir` / `--download`），
安装后同样走「备份 → rsync → 重新加载 → 版本回验」。

安装策略分两种，判据是**目标加载目录是否已被 Chrome 登记**：

- **已登记**（目录里有 manifest.json）⇒ 原地 rsync 覆盖 + 重新加载，
  **全自动，零人工**。日常版本更新都走这条。
- **未登记**（全新机器）⇒ 解压就位后调「加载未打包的扩展程序」。
  这一步**无法全自动**，实测结论（2026-11-13）：ego-browser 能点到按钮，但
  Chrome 对 CDP 合成点击不弹出文件选择器（`waitForFileChooser` 8s 超时），
  macOS 原生目录面板也不受 CDP 键盘事件控制。脚本会点开按钮并尝试接管
  chooser（`tryLoadUnpacked()`），失败则打印一次性人工三步指引
  （点按钮 / 选目录 / 点打开）。此后的版本更新回到全自动。

`--download` 的前提是平台已上架 `browser_extension` 资产（链 C）。当前线上
用户端页面显示「当前部署未接入平台，或平台尚未上架桥接扩展包」、无下载按钮，
脚本会明确报这个错并退出（不是脚本故障）。

### 2.3 为什么必须保留加载目录路径

Chrome 扩展 ID 由加载目录路径决定。**路径变化 = ID 变化 = `chrome.storage` 全丢**
（user-server 地址配置、已发送缓存），已发送缓存丢失可能导致历史消息重复发送。
⇒ 只能原地 rsync + 点「重新加载」，**绝不能移除重装**。
加载目录来源：环境变量 `HIVEBRIDGE_LOAD_DIR` 或 `~/.hivebridge-publish.json` 的 `loadDir`
（个人配置，不提交仓库）。

### 2.4 发版必做（两道门）

1. `package.json` 与 `manifest.json` **同时** bump（055fa098 只 bump 了 manifest 到 1.2.1、
   package.json 停在 1.1.0，导致 `npm run release` 前置门必红、发版链实际不可用；
   1.3.0 已修复并统一）。
2. `scripts/release.mjs` 的 `CHANGELOGS` 补当前版本条目（缺条目直接 exit 1）。
   历史教训：变更说明曾硬编码，1.0.2/1.0.3 两次把上一版改动说成本版改动。

### 2.5 版本史

`1.0.2` 会话锚点 → `1.0.3` 跨仓键漂移修复 + 开箱直连线上 → `1.0.4/1.0.5` 修 build 注入被
MV3 短路 → `1.2.0` 六渠道统一接待 → `1.2.1` 手工 bump manifest（package 漏 bump）→
`1.3.0` 四渠道真机校准修复 + 版本双源一致性修复（35e288db）。

## 3. 链 C：安装包上链平台（阻塞，无法自动）

- 平台端代码**不在本仓**（全仓仅 `user-server/go.mod` 一个 go.mod；
  `AssetTypeBrowserExtension` 定义在平台仓），服务器上也只有编译产物。
- user-server 侧只有只读/购买/拉取/上报用量的客户端
  （`internal/platform/asset_market_client.go`：`ListAssets`/`GetAssetDetail`/`Purchase`/
  `PullData`/`MyPurchases`/`GetCredits`/`ReportUsage`），**没有上传方法**。
- **2026-11-13 实证（服务器 `platform-server` 二进制 strings 全量提取）**：全部路由中
  merchant-api 侧仅 asset-market 的 list/detail/purchase/my-purchases/report-usage/sync；
  platform 侧仅 asset-market 的 all/revenue-summary/pending/contributors/credits/withdrawals
  与 assets 的 batch-approve/batch-publish/{id}/approve/{id}/reject/{id}/offline、playground
  preview-parse/test-run。**零资产上传路由**（无 upload/file-post 类接口）⇒ 不是"缺凭证"，
  而是接口本身不存在，上传只能在平台管理后台 UI 完成。
- ⇒ 上传只能由平台运营操作，把 `user-web/bridge/release/hivebridge-<version>.zip`
  作为 `browser_extension` 资产上传。上传后用户端「桥接扩展安装包」页的版本徽章自动变新
  （平台侧文件由 `work.xapptool.cn` 的 `^~ /files/extensions/` 反代 8205 托管）。
- 平台商户端认证三头（对接平台 API 时用）：`X-Merchant-Key` / `X-Timestamp` /
  `X-Signature`（HMAC，payload = `method\npath\ntimestamp\nbody`，密钥为 merchant key）。
  契约闸：`/merchant-api/`、`/llm-api/v1/`、`/api/platform/` 三组必须带
  `X-Contract-Version: 1`（十进制 "1"，传 "v1" 会按 v0 处理 ⇒ 426）。
- **2026-11-13 现状**：线上 `https://workapi.xapptool.cn/api/system/info` 回
  `{"platform_enabled":true}`（线上已接入平台），但租户端安装包页显示
  「当前部署未接入平台，或平台尚未上架桥接扩展包」⇒ 平台上**没有**已上架的
  browser_extension 资产。**1.3.0 安装包尚未上链**（`user-web/bridge/release/hivebridge-1.3.0.zip`）。

## 4. 收尾清单（每次发布后逐项打勾）

- [ ] 链 A：`deploy/deploy-api.sh` 全绿、容器内 + 公网 `/api/health` 200、`PROD_TOPOLOGY_CHANGES.md` 台账已更新
- [ ] 链 B：`npm run publish -- --reload` 全绿、chrome://extensions 快照版本号 = 目标版本
- [ ] 链 B 租户侧安装：`node scripts/install-extension.mjs --zip release/hivebridge-<v>.zip` 全绿
- [ ] 链 C：安装包已由平台运营上传、租户端页面版本徽章已变（**人工项，无接口可自动化**）

> 2026-11-13 本轮状态：链 A ✅（2d910841 已上线，9 个 user-server 提交追平）；
> 链 B ✅（1.3.0 已装进 Chrome 并重载；安装链执行器 install-extension.mjs 已实测：
> `--zip` 全自动覆盖安装 + 重载、`--download` 在平台无资产时正确报错、互斥门生效）；
> 链 C ⛔ 待平台运营上传 1.3.0。
- [ ] 双远端已同步（`gitee-upstream` + `upstream`）
