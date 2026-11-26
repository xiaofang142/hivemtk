# C02 GEO 主动 UA 探测来源修正

## 根因与修正范围

`RunCrawler` 原先调用本服务的 `http.Client`，自行设置 AI Bot UA；HTTP 2xx/3xx 后写入 `GeoCrawlerVisit`。这既没有让外部引擎来访，也不能证明收录。旧表还混有匿名 UA 观察，来源没有持久化，统计却被展示为真实 AI 访问、覆盖度和声量排名。

本轮只改 C02 的 `CrawlerStats.vue`、独立中英文资源、`internal/geo/` 对应控制器/服务/模型/仓储与回归测试。未操作菜单、其他内容页、现有模拟 `ProbeModeBanner`、现有 zh/en 总目录或 catalog。

## 数据契约

- `GeoCrawlerVisit.source`：`active_ua_probe` 仅由主动请求路径写入；`ua_observation` 表示现有匿名 UA 识别入口的未验证观察；通用无来源入口与存量记录为 `unknown`。没有增加“已验证的外部抓取”分类或日志采集能力。
- 所有六项探测聚合只读取 `source = active_ua_probe`。旧记录不按 UA、IP、关键词或 URL 推断来源，也不进入探测计数。
- 统计响应新增 `data_source`，明确 `kind=active_ua_probe`、`success_criteria=http_2xx_3xx`、`external_crawl_verified=false`、`indexing_verified=false`、`unattributed_records=excluded`。
- UA 条目数 `configured_ua_count` 与按现有 UA 识别器计算的分组数 `configured_engine_groups` 分开返回。21 条 UA 可以归入 8 个分组；分组含识别器的 `other` 桶，不代表独立外部引擎身份。
- 规范计数为 `probe_count`、`probe_share_pct`、`self_site_probe_share_pct`；摘要为 `last_24h_probes`、`self_site_probes`、`competitor_probes`。内部沿用的旧 Go 变量名不改变上述 API 语义。
- 域名比较输出规范的 `is_self_site`，`is_hivemtk` 暂作旧别名，两者值相同。UI 优先使用规范字段，仅在已声明主动探测来源的响应中回退识别旧站点标记；缺少来源元信息的旧响应隐藏计数并解释原因。
- 关键词是本系统任务标签；站点等级来自配置；成功数量占比依赖路径/关键词/UA 采样，均不是搜索排名或品牌声量。
- 手动执行现在返回 HTTP 202 / `status=accepted`，说明仅受理主动 UA 探测。UI 删除固定 30–60 秒完成与定时刷新承诺，改为提示稍后刷新。
- 读取任一统计失败会返回错误，UI 清除旧卡片及列表并展示失败，避免全零“成功”或残留上次读数。

## 迁移与租户

模型已存在于启动 `allModels()`；通过现有 `AutoMigrate` 加来源列，默认 `unknown`，不运行历史数据猜测更新。新增 `(tenant_id, source, created_at)` 索引服务探测查询。

专用 PostgreSQL 测试从旧表形状迁移两次，确认幂等且旧行保持 `unknown`。全部仓储聚合沿用租户上下文和隔离回调。报表与任务入口在多租户缺身份时提前拒绝；已知租户使用 `OnlyTenant` 剥离系统豁免，手动执行只派发当前租户。匿名访问入口继续在多租户模式下跳过。

## 语言资源接入（主助手负责）

新增资源文件的内容为平铺键，需分别注册为 `zh.crawlerSource` / `en.crawlerSource`：

```js
import crawlerSourceZh from './locales/crawlerSource.zh.json'
import crawlerSourceEn from './locales/crawlerSource.en.json'
// 对应语言对象中：crawlerSource: crawlerSourceZh / crawlerSourceEn
```

组件测试独立装配该 namespace，不依赖主助手正在修改的 catalog。

收尾只读确认：主助手已在 `user-web/src/i18n/catalog.js` 按上述结构完成中英文资源注册；本任务没有编辑该文件。`geoProbe.js` 继续复用原端点；现有响应拦截器按 `code=0` 解包，对 HTTP 202 无额外限制。

## 真实验证证据

- 红灯：服务新增 5 项测试全部失败，分别复现主动请求缺来源、匿名观察缺来源、报表缺来源契约、读取失败被吞、缺租户不拒绝。
- 红灯：仓储测试统计出本租户 3 条混合记录而非 1 条主动探测；旧表迁移后没有来源列。
- 红灯：前端来源回归 6 项失败，旧页面仍显示 HiveMTK/声量排名/真实访问说明；新计数字段尚未适配造成渲染错误。
- 绿灯：`npx vitest run tests/unit/crawler_source_contract.test.js && npx eslint src/views/geo/CrawlerStats.vue`：资源最终改为平铺键后的复测为 1 file / 6 tests passed，31.32s，eslint 无输出；覆盖中英证据边界、21 UA/8 分组、旧数据隐藏、新旧本站字段、刷新失败和受理提示。
- 绿灯：`go test ./internal/geo/service ./internal/geo/repository -run 'CrawlerSource|CrawlerVisitService|GeoCrawlerVisitRepository|StatsByDomainFlagsSelfSite|SelfSiteNotKeyedByHost|DomainCompare|SelfBrandSOV' -count=1`：service 1.541s、repository 2.720s 均通过；真实 PostgreSQL 为专用 `127.0.0.1:8217`。
- 绿灯：`go test ./internal/geo/... -count=1`：controller 8.673s、repository 14.966s、service 39.404s 全部通过，其他 GEO 包无测试文件。包含新受理接口的 HTTP 202 / `accepted` 契约及真实服务缺租户拒绝路径。
- 绿灯：`go build ./... && go vet ./...`：无输出，exit 0。
- 绿灯：`npx eslint src/views/geo/CrawlerStats.vue`，无输出，exit 0。
- 绿灯：`python3 scripts/check-layer-boundary.py`：扫描 1785 个非测试 Go 文件，新增跨层 import 0。
- `git diff --check`：exit 0。
- 按全局指令执行 graphify 刷新，但本环境缺模块：`ModuleNotFoundError: No module named 'graphify'`。未安装依赖或生成虚假图谱；本 worktree 无 `graphify-out/`。

本轮没有浏览器真机证据，不据单元测试声称线上页面已验证。

## 操作边界

没有 commit / push / deploy；没有向线上发出爬虫任务、发送消息、购买、重新登录或改浏览器状态。主动 HTTP 单测使用自定义 RoundTripper，受理接口测试使用执行入口替身；真实服务入口仅测“缺租户立即拒绝”路径。
