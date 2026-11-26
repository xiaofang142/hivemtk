# 导航与页面内容发布验收

本记录只登记实际发布与逐元素页面证据，整体回归见 [test.md](test.md)。

## 源码与产物

- 发布源码：`1bb9ead90ad793c1612dbc3b09c1da07e127d71c`，从整合上游后的干净 master 副本构建。
- SPA：`npm run build` 退出 0，37.43 秒；buildId `1bb9ead90-1795658076649`，构建时间 `2026-11-26T01:55:14.291Z`。
- 实际产物 709 个，PWA 预缓存 706 项。最终产物秘密门退出 0：7 枚真值、私有真值命中 0、source map 0、有效 sourceMappingURL 0。
- 本地 index SHA-256：`1e5928abda2c7d5ebf06728b0daf053e94488872baf7cfc3879545e4975dc02f`；入口 `assets/index-C8-bN_lV.js`。
- SPA 发布前完整快照：`/www/wwwroot/hivemtk/user-web.snapshot-navigation-20261126-100505.tgz`，3156441 字节，SHA-256 `34b8bdfbeb268e3fa3c906fbd533f1f8352b3f6b13081b21384d9f62db61f352`。

## 发布状态

实现发布时，双远端 `gitee-upstream/master` 与 `upstream/master` 已独立通过 `git ls-remote` 确认均为上述发布提交；本次收尾在其后补交文档。两端实现推送均执行并通过完整 pre-push 密钥闸。GitHub SSH 断线后使用 IPv4 SSH 443 完成推送；HTTPS 备用通道因现有令牌缺少 workflow 权限而失败，未计为成功。

API 链 A 已于 `2026-11-26T10:20:47+08:00` 发布。全量 Go 测试已通过，本次执行 `deploy/deploy-api.sh --skip-tests`，保留 vet、Linux/amd64 构建、双端摘要、回滚点及健康检查。独立回读宿主二进制、容器 `/app/user-server` 和运行进程 `/proc/1/exe`，三者 SHA-256 均为 `07a38057c9316470049f9ae7df49ce18beae329c9f64d1d49a901424a61f88d4`。运行镜像 `sha256:3d13f536ccd68be906f7e394f71319e304e7337c5e94c4750bf25ee9883d6c9e`，容器启动 `2026-11-26T02:20:36.20078595Z`。内网 `127.0.0.1:8204/api/health` 与正常 TLS 的公网健康检查均 HTTP 200 / code 0 / ok；公开 seed 默认口令返回 HTTP 401，state 为 `ok`。

API 回滚镜像 `mtk-user-server:rollback-20261126-100858`，回滚二进制 `/www/wwwroot/hivemtk/user-server/user-server.bak-20261126-100858`；独立回读的镜像与二进制摘要分别为 `sha256:0630703c75ad8763b0b563662dbf25093c920ca47db311e08282cfdb6ed7f96f`、`8e5c52c053559c6cf4261f6f47b6da7d06fa4bc37f6de8648e132c12a414ecf3`，与发布前基线一致。

SPA 链 D 在上述快照后增量同步至 `/www/wwwroot/hivemtk/user-web/`，保留旧 hash 资源与 `.user.ini`。远端按本地 manifest 逐个读回，709 个文件摘要全部一致、差异 0。正常 TLS 公网抽验 index、version、service worker、入口 JS/CSS、GEO 预览与探测页面、大屏、导览以及中英资源共 12 项，均 HTTP 200 且摘要一致。公网 buildId 为 `1bb9ead90-1795658076649`。

生产库只读核验确认 `geo_crawler_visits.source` 为 `text NOT NULL DEFAULT 'unknown'`，索引 `idx_geo_crawler_probe_window` 为 `(tenant_id, source, created_at)`。历史无来源记录 1,150,460 条仍为 `unknown`，未按猜测改写来源，未发起主动探测。

原始产物核验保存在本机目录 `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/navigation-release-evidence-20261126/` 的 `spa-manifest.json`、`spa-remote-verification.json`、`spa-public-verification.json`。页面快照可能包含业务数据，仅在该本机证据目录保存，不提交正文。

## 页面验证

发布后使用 ego-browser 复用现有管理员登录态，逐个明确操作元素，并在每步后读取页面证据。浏览器服务重启使原 Space 85 及恢复空间失效后，按指南确认旧空间已不存在，再在同一登录态下恢复任务；最终 Space 1 的页面验证已结束并执行 `finish({ keep: [] })`。未重新登录或清理站点数据。

下表证据文件均位于上述本机目录；含业务内容的原始快照不入仓。页面加载过程中的旧快照、超时及未完成动作不计通过。

| 验证项 | 实际结果 | 本机证据文件 |
|---|---|---|
| 八组导航 | 逐个点击八个顶部分组，侧栏随分组更新；工作台默认进入统一收件箱 | `20-group-workbench.txt`、`21-group-crm.txt`、`22-group-agents.txt`、`26-group-outreach.txt`、`27-group-knowledge.txt`、`28-group-geo.txt`、`29-group-analytics.txt`、`30-group-system.txt` |
| CRM 入口与归属 | 客户主数据呈现六个授权页内入口；去重合并页加载完成，面包屑保持客户与销售／客户主数据上下文，未执行扫描或合并 | `04-crm-restored.txt`、`06-crm-dedupe.txt` |
| 线索入口 | 规则页呈现四个治理入口及线索发现智能体入口；点击通知路由后到达目标页面，呈现账号维护人→渠道维护人→租户默认的说明 | `10-leads-ready.txt`、`17-leads-notify-ready.txt` |
| 销售驾驶舱 | 加载结束后显示今日调用次数、账号数量与状态及真实 LTC 口径；页面说明不提供 QPS、吞吐量和错误率 | `19-sales-cockpit-ready.txt` |
| 模块导览 | 从用户菜单启动当前智能体与客服分组导览，显示对应业务说明；明确点击关闭后导览隐藏 | `23-user-guide-menu.txt`、`24-module-tour-started.txt`、`25-module-tour-closed.txt` |
| GEO 访问文件预览 | llms.txt 与 robots.txt 均可生成非空预览；状态为“预览内容 · 未验证”，页面明确线上文件、响应头、规范和覆盖尚未验证 | `32-geo-llms-preview.txt`、`33-geo-llms-regenerated.txt`、`34-geo-robots-preview.txt`、`35-geo-robots-regenerated.txt` |
| GEO 来源与刷新 | 点击只读刷新；页面区分主动 UA 探测、外部抓取与收录，排除无来源历史记录；只读接口 HTTP 200 / code 0，来源为 active_ua_probe | `36-geo-crawler-initial.txt`、`37-geo-crawler-refreshed.txt`、`38-geo-source-contract.json` |
| 大屏示例 | 添加数字卡后显示“—”及“未绑定业务数据源”，保留示例布局说明 | `39-builder-initial.txt`、`40-builder-number-example.txt` |
| 公开确认与取消 | 保存前提示公开链接只展示未绑定数据源的示例布局；点击取消后确认框关闭 | `41-builder-public-confirm.txt`、`42-builder-public-cancelled.txt` |
| 私有确认与取消 | 关闭公开开关后 aria-checked=false；保存前提示非公开示例布局，点击取消后确认框关闭 | `43-builder-private-selected.txt`、`44-builder-private-confirm.txt`、`45-builder-private-cancelled.txt` |

生产 GEO 接口实际返回 `data_source.kind=active_ua_probe`、`success_criteria=http_2xx_3xx`、`external_crawl_verified=false`、`indexing_verified=false`、`unattributed_records=excluded`。当前配置为 8 条 UA、8 个引擎分组、30 天窗口；当前主动探测汇总为 0，与历史 unknown 不纳入统计的页面口径一致。

本次页面验收覆盖上述导航切换与关键交互。权限上界、部分授权、关闭模块、跨模块导览销毁和预览失败重试由前述自动回归及权限门验证；未在生产新建受限角色或注入失败。未执行主动 UA 探测、客户消息发送、购买或大屏实际保存。

## 收尾记录

发布副本的 ignored 部署 state 已同步回主产品仓；同步前确认主仓仍为发布前 `7b4839e6d` 状态且读取期间未变，并将原始字节保存在本机 `main-deploy-state-before-sync.json`，原子替换后回读一致。主仓及原修复工作树的其他改动和暂存区保持原样。

实现已按整体提交 `1bb9ead90` 推送并发布，本次仅补交验收与生产台账。Graphify 缺少本机 Python 模块、覆盖率缺 profile、跨仓端口比对缺相邻对端目录，均保留未执行说明，不计通过。

收尾文档检查：markdownlint 扫描 835 个文件，0 issues；链接检查扫描 848 个已纳入索引的 Markdown 文件，断链 0。修正的 12 处既有格式问题仅涉及加号转义和末尾换行，未改写历史记录的业务含义。
