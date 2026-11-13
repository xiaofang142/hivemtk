---
name: "hivemtk-release"
description: "Publishes HiveMtk changes: chain A deploys the Go API to production, chain B builds+installs+reloads the Chrome bridge extension, chain C uploads the extension zip to the platform. Invoke whenever changes are finished and need to ship — 发布线上/部署/上线/发版, 更新扩展/装进Chrome/重载扩展, 上传安装包/上链平台, release/deploy/publish/ship, or after any change under user-server/ or user-web/bridge/, or when asking how this repo gets changes to users."
---

# HiveMtk 发布链（链 A API 上线 / 链 B 谷歌扩展 / 链 C 平台上链）

代码改完了，怎么让它到用户手里——**只走本 skill 的三条链，别自己另造流程**。

## 0. 先定位仓库（不要在命令里写死个人路径）

```bash
ROOT=$(git rev-parse --show-toplevel)   # 仓根，如 .../go/hivemtk
APP="$ROOT/hivemtk"                     # 主代码：Go 在 user-server/，扩展在 user-web/bridge/
```

后续所有命令都基于 `$APP`，这样在任何编辑器/任何克隆位置都能跑。

## 1. 铁律（用户 2026-11-13 定，违反即返工）

1. **设计了 API 就发布线上**（改完 `user-server/**` 必须走链 A）。
2. **设计了 bridge 就更新谷歌扩展**（改完 `user-web/bridge/**` 必须走链 B）。
3. 三条链**互不替代**；链 C 是链 B 的延伸（让租户也能下载到）。

## 2. 固定流程（SOP）——先判改动范围，再选场景

### 场景 1：只改了 Go API（`user-server/**`）

```bash
cd "$APP/user-server"
go test ./internal/service/ ./internal/bridge/... ./internal/channelgw/...
# ↑ DB 门禁失败（test password is missing）= 环境缺 POSTGRES_TEST_PASSWORD，非代码问题，不阻断
cd "$APP"
./deploy/deploy-api.sh            # 变更驱动：检测到未发布提交才部署
```
收尾：更新 `docs/operations/PROD_TOPOLOGY_CHANGES.md` 台账 → commit + push 双远端
（`gitee-upstream` + `upstream`）。

### 场景 2：只改了扩展（`user-web/bridge/**`）

```bash
cd "$APP/user-web/bridge"
# ① 三处必须同版本：package.json、manifest.json、scripts/release.mjs 的 CHANGELOGS
BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn npm run publish -- --reload
node scripts/install-extension.mjs --zip release/hivebridge-<新版本>.zip   # 可选：租户安装链复核
```

### 场景 3：完整发版（API + 扩展 + 平台上链）

```bash
cd "$APP" && ./deploy/deploy-api.sh                                          # 链 A
cd "$APP/user-web/bridge"
BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn npm run publish -- --reload # 链 B
node scripts/upload-platform-asset.mjs                                       # 链 C
cd "$APP" && git add -A && git commit && git push gitee-upstream master && git push upstream master
```

### 场景 4：换机器 / 新加载目录（全程唯一需要人工的一次性步骤）

见 §5。**稳态版本更新（场景 2）不需要重复这一步。**

## 3. 三个执行器（内部在做什么）

| 链 | 命令 | 机制 |
|---|---|---|
| A | `./deploy/deploy-api.sh` | 预检（master/无未提交/已 push/变更门：对比 `deploy/.api-deploy-state.json` 的 `deployed_sha` 与 `git log <基线>..HEAD -- user-server/`，零改动直接 exit 0）→ go vet + go test → `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` 交叉编译 → scp + 双端 sha256 校验 → 镜像打 `rollback-<时间戳>` 标签 + 二进制 `.bak` 备份 → `docker compose up -d --build user-server` → 容器内 + 公网双健康检查 → 失败自动回滚（二进制 → 镜像标签两级）→ 写台账 |
| B | `npm run publish -- --reload` | 版本一致性门 → 发行口径门（缺 `BRIDGE_ONLINE_SERVER` 拒绝构建，防 localhost 包当线上包发）→ `npm run release` + 产物校验（dist/zip 内 manifest 版本、popup.js 注入串）→ 备份旧加载目录（保留最近 5 份）→ `rsync -a --delete` → ego-browser 打开 `chrome://extensions`、搜索框过滤到唯一卡片、点「重新加载」、快照读回版本号回验 |
| C | `node scripts/upload-platform-asset.mjs` | ego-browser 打开 `https://manage.xapptool.cn/#/platform/asset-market/list`（复用平台运营登录态）→ 页面上下文直调 `POST /platform/asset-market/file-assets`（multipart，Bearer；zip 由父进程读成 base64 经 evaluate argument 传入）→ 调 `batch-publish` 留痕 → 回验 `https://work.xapptool.cn/#/bridge/extension` |

选项：链 A `--dry-run` / `--force` / `--skip-tests`；链 B `--skip-build` / `--dry-run` / `--self-hosted`；
链 C `--zip/--name/--industry/--version/--notes/--dry-run/--no-publish/--no-verify`。

## 4. 必读口径（都是实测踩出来的，违反=发布失败或线上故障）

### 链 A
- 线上是 **Docker**（`mtk-user-server` 容器，`127.0.0.1:8204`），不是文档里旧的 systemd/helm 说法。
- 线上后端版本**没有公开接口可查**（`/api/system/info` 匿名只回 `platform_enabled`；
  `/version.json` 的 buildId 只反映前端）⇒ 只能以 `PROD_TOPOLOGY_CHANGES.md` 台账为准，**不得臆测**。
- 前置：本机 `~/.ssh/config` 有 `mtk-prod` 且 BatchMode 免密可用。

### 链 B
- **发版两道门缺一不可**：①`package.json` 与 `manifest.json` 同时 bump（历史 055fa098 只 bump 了 manifest，
  导致 `npm run release` 前置门必红、发版链实际不可用）；②`scripts/release.mjs` 的 `CHANGELOGS` 补当前版本条目。
- **加载目录路径是硬约束**：Chrome 扩展 ID 由加载目录路径决定，**路径变 = ID 变 = `chrome.storage` 全丢**
  （user-server 地址配置、已发送缓存）⇒ 已发送缓存丢失可能造成历史消息重复发送。
  **只能原地 rsync + 点「重新加载」，绝不能移除重装。**
- 加载目录来源：环境变量 `HIVEBRIDGE_LOAD_DIR` 或 `~/.hivebridge-publish.json` 的 `loadDir`（个人配置，不入库）。
- ego-browser 两个工程约束（改脚本必读）：子进程 `console.log` 会透传父终端、`execFileSync` 的 stdout
  捕获为空 ⇒ **状态必须写文件回传**；`nodejs` 不接受脚本文件路径参数 ⇒ 必须 heredoc 或 `-e <script>`。

### 链 C
- 平台上传路由在 **admin 侧** `POST /platform/asset-market/file-assets`（Bearer JWT，字段
  `file/name/industry/version/changelog`）；merchant-api **没有**上传能力。
- **平台后台「上传扩展包」按钮本身是坏的**（bundle `f=t=>e.upload(...)` 而 multipart 能力在 `l.upload` 上，
  `e` 是裸 axios 实例 ⇒ **真人点也报 `e.upload is not a function` 且零网络请求**）⇒ 只能直调 API 绕开。
- **purchase 门**：平台 detail 的 `version_info` **只在资产 `purchased=true` 后才返回
  `file_url/filename/file_sha256/size_bytes`**；未购买时只有 `{changelog, created_at, schema_version, version}`
  ⇒ `user-server/internal/service/bridge_extension.go` 的 `Latest()` 必须先 `Purchase` 再 `GetAssetDetail`
  （免费资产 price=0，零成本幂等）。
- **不能取 list[0]**：平台 `ListAssets` 按 `updated_at` 倒序，而 purchase 会刷新 `updated_at`
  ⇒ 必须按 `latest_version` 语义比较挑最新（`latestExtensionAssetID`）。
- `POST /api/v1/asset-market/sync` 恒回 `{"code":5000,...}` ⇒ sync 路径不可用，不要走。
- **从 manage.xapptool.cn 页面上下文调 `/merchant-api/...` 会拿到 SPA index.html**（nginx vhost 分流）
  ⇒ merchant-api 只能在容器内或经 user-server 代理端点调。
- 上传接口对同名资产**新建 asset**（不给旧资产加版本），旧资产需在后台「下架」。

## 5. 场景 4 详解：本机固定安装地址（唯一需人工的一次性步骤）

**首次加载新目录无法全自动**（2026-11-13 实测，勿再试探）：
CDP 合成点击 chrome://extensions「加载未打包的扩展程序」不弹文件选择器
（`waitForFileChooser` 8s 超时）；ego-browser 的 CDP 白名单无 `Page.handleFileChooser`；
macOS 原生面板不受 CDP 键盘控制；AppleScript 路也不通（osascript 有权限，但 ego-browser 渲染在
isolated Space 的不可见窗口，System Events 拿不到按钮坐标）；TarsX.app 不在 TCC.db，辅助功能面板搜不到。

⇒ **本机固定安装目录（在「加载未打包的扩展程序」面板里选它）**：

```
/Users/xiaofang/Documents/tool/hivebridge-1.0.2
```

人工一次性三步：`chrome://extensions/` → 开「开发者模式」→ 点「加载未打包的扩展程序」→ 选上面目录 → 「打开」。
目录名里的 `1.0.2` 是历史误导命名，**实际内容就是当前版本**（以目录内 `manifest.json` 的 `version` 为准）。
新发行产物：`user-web/bridge/release/hivebridge-<版本>.zip`。此后所有版本更新回到全自动。

## 6. 深度文档与源码

- 权威文档（含全部历史证据与拓扑台账）：
  `$APP/docs/operations/RELEASE-PIPELINE.md`、`$APP/docs/operations/PROD_TOPOLOGY_CHANGES.md`
- 执行器源码：`$APP/deploy/deploy-api.sh`、`$APP/user-web/bridge/scripts/{publish,install-extension,upload-platform-asset}.mjs`、
  `$APP/user-web/bridge/scripts/lib/{ego-browser,platform-asset}.mjs`
- 服务侧下载链：`$APP/user-server/internal/service/bridge_extension.go`（含 purchase 门与选最新版本的单测）

## 7. 常见误触发与边界

- 只改前端 `user-web/src/**`（管理端页面）⇒ **不属于本 skill**，不走这三条链，正常 commit+push 即可
  （线上 SPA 由 nginx 直接伺服 `user-web` 目录，需另行同步）。
- 问「线上现在是什么版本」⇒ 不要猜，读 `PROD_TOPOLOGY_CHANGES.md` 台账。
- `go test` 报 `test password is missing` ⇒ 环境问题，不是回归，不要当作发布阻塞。
