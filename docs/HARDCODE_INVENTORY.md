# 硬编码点位全量清单（HARDCODE_INVENTORY）

> 生成方式：`scripts/hardcode_sweep.py`（Go 侧）+ `scripts/hardcode_sweep_web.py`（user-web 侧）
> → `scripts/hardcode_classify.py`（Go CONFIG 归类）+ `scripts/hardcode_inventory.py`（合并出清单）
> 机读全量：`hardcode-sweep/INVENTORY.csv`（含表头共 7905 行 ＝ **7904 个点位行**，每行一个点位，
> 带 `file:line` 与建议 `group/key`）
> 本文只承载**口径、汇总、争议点位和施工指引**，逐行明细看 CSV。

---

## 0. 怎么复现这份清单

```bash
cd hivemtk
python3 scripts/hardcode_sweep.py           # Go 侧 → hardcode-sweep/hardcode-sweep.json|md
python3 scripts/hardcode_sweep_web.py       # user-web 侧 → hardcode-sweep/hardcode-sweep-web.json|md
python3 scripts/hardcode_classify.py        # Go CONFIG 五分归类 → hardcode-sweep-classified.json
python3 scripts/hardcode_inventory.py       # 合并 + 指路 → INVENTORY.csv
python3 scripts/check-config-param-readpoints.py   # §7.1 的 G1 门（只读，不碰库）
bash    scripts/check-config-param-readpoints.test.sh  # G1 的 12 枚控制（假树夹具，不碰工作树）
python3 scripts/check-text-encoding.py             # §7.1 的 G7 门
bash    scripts/check-text-encoding.test.sh        # G7 的 21 枚控制（假树夹具，不碰工作树）
```

八条命令都不改生产代码、不动数据库、不联网，但**前四条会重写 `hardcode-sweep/` 下的六份产物**
（想只读就把最后一条换成 `python3 scripts/hardcode_inventory.py --summary`）。
`check-*` 那四条重扫门与**门自己的牙齿**（门绿不等于门有牙，怀疑门坏了先跑那两枚 `.test.sh`）。

**产物的生成面必须与它所在的那个提交同面**。本仓这批六份产物生成于
tip `15ccabb7` 的 `git clone --shared` 影子树＋本泳道 45 个改动文件（`/tmp/r83-shadow/hivemtk`，
2026-10-13 08:35 现跑），并在同一棵树里把四条命令重跑第二遍作**不动点验证**：六份产物 `cmp` 全 SAME
（08:41 现测）。上一批产物生成于 tip `f7381630` 的同一面，两批的差异只有一处、且完全来自 tip：
Go 文件面 1757 → **1758**（他们入库的 `internal/repository/data_scope_owner.go`），
Go `CN` 12533 → **12534**（他们入库的 `internal/controller/inbox.go:385`
`response.Error(ctx, http.StatusForbidden, "无权查看他人的负载")`，落 `API_MESSAGE` 桶）。
其余读数（CONFIG 910 / DATA 79 / INLINE_NUM 1459 / 五分归类 / web 全套 / CSV 7904 行）两批逐字相同。
不在工作树面上生成——工作树多并行泳道的在途件，会把还没入库的代码烤进清单。
tip 每动一次，这一面就要重取一次；本节的读数只在写明的这个 tip 上成立。

> **对账这份 CSV 时的一个字节级坑（2026-10-13 现测）**：`hardcode_inventory.py:439` 用
> `open(..., newline="", encoding="utf-8-sig")` 写文件，`csv` 的默认行分隔是 `\r\n`，
> 而 `.gitattributes:5` 是 `* text=auto eol=lf` ⇒ **库内副本会被归一成 LF**。
> 现测同一份产物：工作盘上的 `INVENTORY.csv` `wc -l`＝7905、CR 字节数＝7905；
> `git show HEAD:hardcode-sweep/INVENTORY.csv` 的 `wc -l`＝7746、CR 字节数＝**0**。
> 所以**别拿 `cmp`／`diff` 直接把生成件对 `git show HEAD:`**——那样永远"全文件不同"，
> 读起来像谁把整份清单重写了。要么先 `tr -d '\r'` 再比，要么只对**工作树 vs 影子树**这两份
> 同形态的产物比（本节的不动点验证就是这么做的，六份全 SAME）。
> 行数的口径也统一写清：`wc -l` 7905 ＝ 1 行表头 ＋ **7904 个点位行**（`csv.DictReader` 数出来同为 7904，
> 分侧 `user-server 489 ＋ user-web 7415`）。

> 影子树的落点曾在 2026-10-13 抓出扫描器自己的一处坏法：`hardcode_sweep.py` 的 `SKIP_TREE`
> 原先拿**绝对路径**比 `/tmp/` 这类段，仓库一旦被克隆到 `/tmp` 下（做干净检出复测的默认落点），
> 每条路径都命中 ⇒ 文件面归零、脚本照退 0、照写出「全仓没有硬编码」的空清单
> （实测打印 `文件面: 0 个`、rc=0，下游 classify／inventory 照跑照写，同一趟六份产物的
> `git diff --stat` 是 `1643 insertions / 96433 deletions`，其中 `INVENTORY.csv` 808 行、
> `hardcode-sweep-classified.json` 7805 行——一份"全仓没有硬编码"的空清单就这么静默生成了）。
> 现改为按**相对仓库根**的 posix 路径匹配（段表一字未动，仓库内的 `tmp/` 照旧排除），
> 并在文件面为 0 时 **rc=2 且不写任何产物**；反向实测：`--repo` 指空目录 ⇒ rc=2、`--out` 目录为空。
> 顺带把原先**声明了却没被读过**的 `--repo` 参数接上（它此前不影响任何路径）。

---

## 1. 总览

> **本节的「起点快照」与「复扫」两组数不能直接相减。** 起点是 2026-10-10 首次全量扫描，
> 复扫是 2026-10-12 在**同一套脚本**上重跑；中间判据改过（兜底实参从「按常量名猜」改成
> 「看它是不是坐在 `Get*(ctx, group, key, 兜底位)` 的第 4 个实参上」，见 §7 的 G4/G3 段），
> 所以桶的边界动了。差值里只有一部分是「真的多接了几条」，其余是同一批点位被重新归类。
> 每个数都可以用 §0 那五条命令现复现，复扫列就是最后一次跑的读数。

### 1.1 起点快照（2026-10-10）

扫描面：`user-server/internal` + `user-server/cmd` 共 **1814** 个 Go 文件（非测试、非种子）；
`user-web/src` 共 **479** 个文件。

| # | 桶 | 原始量 | 去重后 | 治理结论 |
| --- | --- | --- | --- | --- |
| A | Go `CONFIG` → **TUNABLE** | 403 行 | **346** 个 `group/key` | ✅ 应进 `config_params`（含 58 条 P0） |
| B | Go `CONFIG` → PROTOCOL | 20 | 20 | ❌ 不迁（错误码/协议版本，动了就破坏兼容） |
| C | Go `CONFIG` → DEPLOY | 13 | 13 | ❌ 不迁（端口/连接池/DSN，属部署期 env） |
| D | Go `CONFIG` → INFRA | 8 | 8 | ⚠️ 半迁（向量维度/HNSW 需重建索引，须走变更单） |
| E | Go `CONFIG` → FIELD_LIT | 460 | — | ❌ 假阳性：是业务字段赋值（`FromCache=true`、`InboxCreated=true`），不是阈值 |
| F | Go `DATA` → DICTIONARY | 44 | 44 | ⚠️ 另立「字典表」模型，不进 `config_params` |
| G | Go `CN` | 12495 | — | 见 §4，走 i18n 不走配置 |
| H | Go `INLINE_NUM` | 1430 | — | ❌ 不治理（噪声为主，见 §6） |
| I | web `CONFIG` → TUNABLE | 65 行 | 15 真阈值 + 50 需人工筛 | ✅ 真阈值部分应进 `config_params` |
| J | web `CN` | 7237（用户可见 3793） | — | 见 §4 |

**可行动口径**：Go 侧 TUNABLE 去重 **346** 个点位 + 前端真阈值 **15** 个 ≈ **361 个**点位应进参数中心。
其中 **P0 = 58**、**P1 = 130**、**P2 = 259**（详见 CSV 的 `priority` 列）。

CSV 有意**不去噪**：宁可多列让人筛，也不让扫描器替人做判断。按人工复核的经验，
`frontend_ui` 那 50 行里约 30 行是 `i = 0` / `hash = 0` 这类循环下标假阳性，
所以上面表里我给了「15 真阈值」这个保守数。

### 1.2 复扫（2026-10-12）

扫描面：Go **1751** 个文件（脚本口径：`internal` + `cmd`，排 `_test.go`/`_seeds.go`/`_seed.go`
与 `cmd/seed`、`/vendor/`、`/migrations/`、`/test/`、`/tests/`、`/tmp/`、`/backups/`；
2026-10-13 起这些段按**相对仓库根**的路径匹配，见 §0 的注，段表本身一字未动）；
`user-web/src` **489** 个文件。2026-10-13 在干净克隆面重算为 **1757 / 488**，差的分别
是并行泳道已入库的 6 个 Go 文件与被 `user-web/.gitignore` 忽略的 1 个本机文件（下表末列有账）；
同一天 tip 再动四笔后，本批产物所在面重算为 **1758 / 488**（多的那 1 个文件见 §0 注）。
起点那句「1814 个文件」用上面这套口径**复算不出来**（2026-10-12 在开发机上算 1751、
2026-10-13 在干净克隆面算 1757）——它是一次没被脚本
记录过的手工计数，本轮起改为按脚本口径记账，文件面数不参与任何门禁判定。

复扫读数（与两个已提交版本同口径对齐；每格都可以用 §0 的命令现复现，末列是 2026-10-13 08:35 在
`15ccabb7` 影子面重新生成这批产物时的读数——**产物就是按末列那一面提交的**）：

| 量 | `36483f93`（2026-10-10 起点） | `103f6dbb`（阶段六收口，当时的 HEAD） | 工作树（2026-10-12 终态复扫） | `f7381630` 影子面（2026-10-13 上一批产物） | `15ccabb7` 影子面（2026-10-13 本批产物） |
| --- | --- | --- | --- | --- | --- |
| Go 文件面 | — | — | 1751 | 1757 | **1758** |
| Go `CONFIG` 声明行 | 839 | 866 | **910** | 910 | 910 |
| Go 五分归类 | FIELD_LIT 460 / TUNABLE 338 / PROTOCOL 20 / DEPLOY 13 / INFRA 8 | 455 / **370** / 20 / 13 / 8 | 459 / **378** / **51** / 13 / **9** | 459 / 378 / 51 / 13 / 9 | 459 / 378 / 51 / 13 / 9 |
| Go `DATA` | 80 | 77 | **78** | **79** | 79 |
| Go `CN` | 12495 | 12256 | **12505** | **12533** | **12534** |
| Go `INLINE_NUM` | 1430 | 1453 | **1458** | **1459** | 1459 |
| web 文件面 | 479 | 485 | **489** | **488** | 488 |
| web `CN`（用户可见属性） | 7237（3793） | 7224（3771） | **7348（3843）** | 7348（3843） | 7348（3843） |
| web `CONFIG` | 65 | 67 | **67** | 67 | 67 |
| web `DATA` / `URL` / `TUNE` | 524 / 0 / 1752 | 528 / 0 / 1730 | **533 / 0 / 1754** | 533 / 0 / 1754 | 533 / 0 / 1754 |
| 清单 CSV 点位行 | 7725 | 7745 | **7904** | 7904 | 7904 |

**倒数第二列相对第四列只动了 5 格，且分两种机制、都不是"清单口径变了"**：
① Go 面 `1751 → 1757`、`DATA +1`、`CN +28`、`INLINE_NUM +1` 全部来自并行泳道
`04dfb079`／`08984776` 两笔**已入库**的新代码（如 `internal/model/menu_capability_routes.go:29`
的 `MenuCapabilities` 映射字面量是那条 `DATA +1`）；
② web 面 `489 → 488` 掉的正是 `user-web/src/config/http.js`——它被 `user-web/.gitignore:70`
忽略，只活在开发机上，干净克隆里没有它。**这一格说明第四列（工作树面）本来就不该当产物面用**。
而 CSV 与 `hardcode-sweep-classified.json` 在两面上 `cmp` **逐字节相同**：classify 只吃原始
sweep 的 `config_consts` 一段（`hardcode_classify.py:112-115`），`DATA`／`CN`／`INLINE_NUM`
三桶不进 CSV，所以那 30 条漂移碰不到清单口径。

**末列（本批产物面）相对倒数第二列只动 2 格，且两格都只来自 tip**：Go 文件面 `1757 → 1758`、
Go `CN` `12533 → 12534`。两格是并行泳道两笔**已入库**代码留下的：`957af95c` 新增
`internal/repository/data_scope_owner.go`（多 1 个 Go 文件，本身不产任何点位），`298da87b` 在
`internal/controller/inbox.go:385` 加了一条 403 文案（多 1 条 `API_MESSAGE`）。
其余 11 格逐字相同；web 那两份产物（`hardcode-sweep-web.json|md`）、`classified.json` 与
`INVENTORY.csv` 在新面上 `cmp` 也全 SAME——并行泳道这几天没动过 `user-web/src`，
而 Go 侧那两处都落在不进清单的桶里。

末列相对 `103f6dbb` 动了三格（倒数第二列在这三格上读数一字未动，所以这段对两列都成立），
**三格都是扫描器口径的形状修复，不是代码长出来的**
（HEAD 产物→工作树产物逐三元组对账，`sweep.config_consts` 与 `classified.rows` 同为 866 → 910：
**新增 54 行、丢弃 10 行、跨桶搬移 0 行**；按**名字**去重则是 **47 增 / 3 删**，
差额是 **7 对"同一名字换了行号"**（本轮给 `handler_http.go`、`dialog_manager.go` 等文件补注释
把常量往下推了几行，行级对账会把它们读成一删一增，所以两个数都得写）：

1. **`CONFIG` 声明行 866 → 910**：这 **+44 要拆成三笔**才不出假账——
   866 是 HEAD **产物**里的数，而那份产物比它所在的那个提交更旧（第 3 点里那三条常量在
   `103f6dbb` 就已删除）；用 HEAD 那版脚本扫当前源码得 **869**（差的 3 就是这三条），
   本轮把声明面补齐后再扫得 **910**，即 **+41 行**。这 41 行里 **+40** 来自并入锚定声明式、
   **+1** 来自本轮最后补的一处值形状（见下）。
   `hardcode_sweep.py` 的常量面原先只认
   `Ident = 字面量`，带**类型标注**的声明（`MaxWebhookBody int64 = 2 << 20`）整类不进视野。
   本轮不是改写那条正则（它有既成事实：不锚行首时 `AND is_active = true` 会被当成常量声明，
   实测一锚就冒出 SQL 语句假阳性），而是**并上一条与 G4 的 `CONST_DECL` 同形的锚定声明式**、
   两个循环各扫一遍再合并。
   这一步是**三方对齐**的必要条件：sweep 的 `CONST_DECL`、G4 的 `CONST_DECL`、G3 的 `NUM_CONST`
   此前只有 G3 认类型标注（它的 `NUM_CONST` 注释早就写着认 `var X time.Duration = …`）。
   不对齐的后果不是读数难看，而是 **G4 会永久红**：`--refresh-baseline` 生成的基线来自
   classify 的 TUNABLE 集合，而 classify 的输入来自 sweep——扫不到的带类型旋钮永远进不了基线，
   而 G4 那边一眼就看见它在源码里。
   那 **+1** 是同一类缺陷的另一个形状：sweep 的 `NUM_LIT` 写成 `^[\d.]+$`，
   于是 Go 的下划线分隔整数字面量（`const DefaultMaxKeys = 10_000`）被折成"解不出"、
   整行不进 `config_consts`；G4 的值形状 `\d[\w.]*` **早就认它**。
   实测全仓这种常量**只有 1 条**（`internal/cache/memory.go:16`），
   但对齐的代价是一个字符，不对齐的代价是「门看得见、台账看不见」——
   本清单 §7 已经为「两边口径必须一致」写过一条，这条是它漏下的一个面。
2. **`PROTOCOL` 20 → 51（+31）**：上面那条把带类型的枚举成员放进了声明面，
   `ClueTypeQQ = 1`、`OrderStatusPending = 0`、`CrisisLow = 1`、`AccountStatusActive = 1`
   这类**协议/状态枚举成员**第一次以「成员名」身份进表（原先只以「类型名」身份进，
   而类型名会被归类器的字段正则丢掉）。它们不是新旋钮，归类必须跟着改，
   所以给 `hardcode_classify.py` 的 `PROTOCOL_HINT` 补了成员名族
   （`Crisis*` / `UserStatus*` / `OrderStatus*` / `AccountStatus*` / `ClueType*` / `QuoteVersion*` / `EmailStatus*`）。
   **这一格的增减没有一条是治理进展**：51 条里 31 条是本轮才看得见的既有对外契约。
   同时 `INFRA` 8 → 9（新看见的 `pgvector_index_ops.go: HNSWMinRowsForIndex int64 = 50000`，
   与同文件那四条 HNSW 参数同族）、`FIELD_LIT` 455 → 459（`type.go` 的四个 `SmlistType*` 成员名不匹配任何词缀）。
3. **丢弃的 3 条名**（行级 10 条里去掉上面那 7 对换行号的）：`HTTPPollingMaxTimeout` /
   `HTTPPollingDefaultTimeout` 是本轮删掉的死代码常量（`bridge/handler_http.go:32` 注释记着下架缘由），
   `WeComErrorRateDegradeThreshold` 早在 `103f6dbb` 就随 wecom 分档拆分删除——
   **它出现在 HEAD 的产物里，说明那次提交没重跑扫描管线**（已入库产物的时间戳可以晚于它声称代表的代码状态，
   引用旧产物读数前先跑一遍 §0）。

三段里唯一有治理含义的移动是 Go 归类行的增减，其余是代码自身在长（新增文案、新增常量）。
起点表 A 行写的「403 行 → 346 去重」在 `36483f93` 的产物里**找不到对应数**
（该版 classify 的 TUNABLE 是 338 行、CSV 的 Go `TUNABLE` 桶也是 338），
所以从本节起以产物为准，起点表只作为「当初看到什么」的记录保留。

web 的 67 条 `CONFIG` 全是命名常量（`frontend_ui` 52 + `frontend_ws` 15）。
本轮给清单补了**前端的「已接」判据**：此前只有 Go 侧认兜底实参位，前端 67 条被整批记成待接，
而其中一批早就从 `/api/config/runtime` 读值了——台账把做完的活记成欠账。
现在的形状是：常量坐在 `rtNum`/`rtDur`/`rtBool`/`rtStr(group, key, 兜底位)` 的第 3 个实参上即 `WIRED`，
**17 行已接**（`agentSocket.js` 5 + `chatSocket.js` 5 + `journeyTracker.js` 3 +
四个视图组件各 1——`approvalTask/List.vue`、`badCase/List.vue`、`browserAutomation/Monitor.vue`、
`customerSession/List.vue`；去重后 12 个常量名），**50 行待接**（P0 3 + P1 2 + P2 45；P2 那 45 行仍是 `i = 0` /
`hasCycle = false` 这类待筛噪声）。第三个实参是字面量的**不**算已接：
`MaterialLibrary.vue:329` 的 `rtNum('frontend_ui', 'material_max_size_mb', 10)` 就是这种——
参数已经能读到了，但值写在调用点，`maxSize` 这条仍是硬编码点位。

机读全量：`hardcode-sweep/INVENTORY.csv` 共 **7905 行**（表头 + **7904** 个点位）。

| 清单桶 | 行数 | 含义 |
| --- | --- | --- |
| `TUNABLE`（user-server） | **258** | 该进参数中心、**还没接**的常量 |
| `WIRED`（user-server） | **89** | 常量正坐在 `Get*` 的兜底实参位上 = 已接 |
| `PARAM_FALLBACK` | **31** | 只作为兜底实参出现，本身不是待迁点位（与上一条一起算 DONE） |
| `DICTIONARY` / `PROTOCOL` / `DEPLOY` / `INFRA` | 38 / **51** / 13 / **9** | 另立字典表 / 不迁 / 部署期 env / 走变更单 |
| `TUNABLE`（user-web） | **50** | 前端候选，其中 5 行（P0 3 + P1 2）值得先动 |
| `WIRED`（user-web） | **17** | 常量坐在 `rt*` 的兜底实参位 = 已接（值来自 `/api/config/runtime`） |
| `API_MESSAGE` / `AI_OUTPUT_REPLY` | 7322 / 26 | 走 i18n，不走配置 |

**对账（这两笔必须相等，是清单自身的自检）**：`user-server` **489** + `user-web` 7415 = 7904；
带优先级的 **419** = `TUNABLE` 258+50 + `DICTIONARY` 38 + `PROTOCOL` **51** + `DEPLOY` 13 + `INFRA` **9**
= 7904 − `DONE` **137** − `I18N-A` 7322 − `I18N-B` 26。
`DONE` 的 137 恰好 = `WIRED` 89+17 + `PARAM_FALLBACK` **31**。
`user-server` 的 489 = `CONFIG` 451 + `DATA` 38。那 451 与归类五分（和为 910 **声明行**）的差额
就是 `FIELD_LIT` 459：它是赋值噪声，**不进清单**。所以 910 − 459 = 451，加 `DATA` 38 = 489 ✓。
（`DICTIONARY` 这两轮的两次移动都是**扫描器自己的口径缺陷**，不是代码变了形状：
一次是 `LTCKnownThresholdKeys` 先前被键名词缀表漏掉——那张表没有 `Keys` 词缀，
于是台账里列着的这条键名清单在 CSV 里根本不存在，补词缀后实测**只多命中这一行**，
其余 34 条被丢弃的 DATA 行一条没被捞进来；
一次是那 44 行里有 **6 行根本不是字典声明**——`dialog_manager.go:363,366,369,372,375,378`
的 `intent.Categories = []string{…}` 是结构体字段赋值，因为字段名恰好叫 `Categories`
而被同一张词缀表捞进「业务字典」桶。现要求等号左侧是一个裸标识名（可带 `var`/`const`）
才算字典声明，实测**恰好少这 6 行、其余 38 行一行没动**，见 §3。）

| 版本 | `DONE` | P0 | P1 | P2 | P3 |
| --- | --- | --- | --- | --- | --- |
| `36483f93` | 无此列 | 58 | 130 | 259 | 41 |
| `103f6dbb` | 92 | 45 | 106 | 237 | 41 |
| 工作树 | **137** | **31** | **89** | **226** | **73** |

P0/P1/P2 三档一路在降，**主要不是治理退步，也不是治理进步，而是记账口径搬家**：
`DONE` 这一列是 HEAD 才引入的（已接线与只做兜底实参的点位不再占优先级），
本轮 `WIRED` 14 → 89、`PARAM_FALLBACK` 78 → 31 的相互搬移则来自 G4 判据换了形状
（从「常量名像不像阈值」改成「它是不是坐在 `Get*(ctx, group, key, 兜底位)` 的第 4 个实参上」，见 §7）。

**HEAD→工作树的行级对账**（`hardcode-sweep/INVENTORY.csv` 按 `(side, file, line, name, kind)`
对齐非 CN 行，521 → 556 行，键唯一无重复）：
**新增 45、删除 10、桶间搬移 85**。45 笔新增按桶是 `PROTOCOL` 31 / `WIRED` 8 / `TUNABLE` 3 /
`INFRA` 1 / `PARAM_FALLBACK` 1 / `DICTIONARY` 1；10 笔删除是 `DICTIONARY` 6（就是下面 §3 那 6 行
`intent.Categories` 假阳性）/ `TUNABLE` 3（第 3 点那三条已不存在的常量）/ `WIRED` 1
（`WeComQuotaDegradeThreshold` 换了行号，与新增里同名那条配成一对）。
85 笔搬移只有两个方向——
`PARAM_FALLBACK → WIRED` 48 笔（同一条常量既坐兜底位、名字又带 `Default*` 前缀，
新判据按位置把它记成"已接"而不是"只作兜底"），`TUNABLE → WIRED` 37 笔
（P0 13 / P1 19 / P2 5；这批**原本就接了**，是老判据认不出 `pkg.Const`、`int(pkg.X)`、
`pkg.X*time.Second` 三种兜底实参形态）。于是三档的降与升全部可解释：
`P0 45 → 31` = 13 笔认成已接 + 1 笔删除（`WeComErrorRateDegradeThreshold`）；
`P1 106 → 89` = −19 笔认成已接 −1 笔删除（`HTTPPollingMaxTimeout`）+3 笔**新可见**
（`telegram.go:737 MaxDownloadFileBytes`、`inbox_ingress.go:65 InboxRecheckBudgetMax`、
`webhook_dedup.go:262 MaxWebhookBody`——三条带类型/位移表达的大小写旋钮，
补齐声明面后才进视野，**登记为存量而非本轮接线**）；
`P2（TUNABLE）194 → 188` = −5 笔认成已接 −1 笔删除（`HTTPPollingDefaultTimeout`）；
`P3 41 → 73` = `PROTOCOL` +31、`INFRA` +1，全是 §1.2 归因第 2 点里"本轮才看见的既有对外契约"，
**不是治理倒退**（P3 的判据本来就是"不迁，但记着为什么"）。
前端那 17 行是本轮新认的口径（上一条段落），HEAD 的账里前端一条 `DONE` 都没有。

**当前可行动口径**：Go 侧 **258** 个未迁点位（`misc` 78、`knowledge` 38、`agent_llm` 28、
`confidence` 25、`workflow` 25、`telemetry` 23、其余 9 组合计 41）+ 前端 **50** 行候选
（其中 P0 3 + P1 2 = **5** 行值得先动，其余 45 行待人工筛）；
全表未接点位 P0 = **31**、P1 = **89**、P2 = 226、P3 = **73**（详见 CSV 的 `priority` 列）。

---

## 2. A 桶：应进 `config_params` 的点位

完整表见 CSV。**注意单位**：下面这张复扫表数的是 CSV 的**点位行**（一行一个常量声明位，
`file:line` 唯一），不是起点表那种 `group/key` 去重数——同一个键在两个文件里各坐一个兜底位，
这里就是 2 行。起点表保留在下面，只作「当初按去重口径看到什么」的记录。

**复扫分布（2026-10-12 终态，Go 侧 `CONFIG` 可迁量 378 行 = 未接 258 + 已接 120）**

| group | 未接 | 已接 | 合计 | group | 未接 | 已接 | 合计 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `misc` | 78 | 31 | 109 | `agent_tool` | 6 | 7 | 13 |
| `knowledge` | 38 | 10 | 48 | `human_task` | 4 | 8 | 12 |
| `workflow` | 25 | 16 | 41 | `wecom` | 4 | 6 | 10 |
| `agent_llm` | 28 | 8 | 36 | `channelgw` | 4 | 5 | 9 |
| `confidence` | 25 | 5 | 30 | `bridge` | **0** | 7 | 7 |
| `telemetry` | 23 | 1 | 24 | `pagination` | 3 | 3 | 6 |
| `inbox_sales` | 9 | 8 | 17 | `reach` | 6 | 0 | 6 |
| | | | | `middleware` | 1 | 4 | 5 |
| | | | | `sales` | 4 | 0 | 4 |
| | | | | `cache` | 0 | 1 | 1 |

这张表是 CSV 的现测分布（`bucket` ∈ `TUNABLE` 记未接，`WIRED`∪`PARAM_FALLBACK` 记已接）。
`cache` 那一行是本轮**补扫描器形状**补出来的、不是新接的：`DefaultMaxKeys = 10_000`
因为下划线字面量解不出而整行不进视野（§1.2 归因第 1 点末尾），而它其实一直坐在
`internal/app/cache_params_wiring.go:33` 的兜底实参位上——台账把它记成"不存在"，
差的就是那一个字符的正则。
本轮早些时候这里记的是 373 行，那一版是**补齐声明面之前**的中间态，已被本表取代；
与 HEAD 的行级增减/搬移对账见 §1.2，逐行名单看 CSV 的 `bucket` 列。

`bridge` 是第一个「未接归零」的组：7 行全部坐在兜底实参位上（起点表里它还有 10 个去重点位，
差的是同名常量合并）。前端侧 `frontend_ui` 52 + `frontend_ws` 15 = 67 行里，
本轮按 `rt*` 兜底实参位认出 **17 行已接**，剩 **50 行**待接（P0 3 + P1 2 + P2 45，§1.2）。

**起点分布快照（2026-10-10，A 桶 403 行去重后 346 个 `group/key`）**

| group | 唯一点位 | group | 唯一点位 |
| --- | --- | --- | --- |
| `misc` | 74 | `bridge` | 10 |
| `knowledge` | 33 | `channelgw` | 8 |
| `frontend_ui` | 50（≈15 真） | `wecom` | 7 |
| `agent_llm` | 20（`alignment_stage` 五维评分是同一份映射表，实际 6 个） | `pagination` | 6 |
| `workflow` | 16（原记「`ALLOW_INSECURE_WEBHOOK` 14 行是同一个开关」，实测是 3 个读取点、其余为文案提及，见 §2.3②） | `reach` | 6 |
| `confidence` | 27 | `sales` | 4 |
| `telemetry` | 22 | `middleware` | 3 |
| `inbox_sales` | 16 | `frontend_ws` | 11（两个 socket 工具各有同名项） |
| `human_task` | 14 | | |
| `agent_tool` | 10 | | |

### 2.1 P0（起点 58 个 → 现在 31 个）：安全、上传、并发上限、TTL 到期

P0 判据 = 「改了以后**一定**有人想改，而且改错会出事」。分四类：

**① 上传大小上限 —— 三份互相矛盾的副本（本次扫描发现的最硬实证）**

> 下面这张表和紧随的那段「已核实」**记的是接线前的状态**（2026-10-10 首扫时读到什么）；
> 接线结果与**今天**的行号在后面的 ✅ 块里，两处不要混着读。

| 位置（接线前） | 值 | 性质 |
| --- | --- | --- |
| `internal/controller/upload.go:24` | `MaxUploadSize = 10 * 1024 * 1024` | 编译期常量，**实际生效的就是它** |
| `internal/model/system_config.go:21` + `internal/service/system_config.go:43,57,105` | `MaxUploadSizeMB = 50` | 运维在「系统配置」页可改，`gorm:"default:50"` |
| `user-web/src/views/system/MaterialLibrary.vue`（原 `:323`） | `maxSize = 10` | 前端又写死一份 10 |

已核实：`MaxUploadSizeMB` 全仓只有 model / service 保存时校验 + controller 空 body 判断这 4 处读它，
**上传链路一处都不读**。也就是说运维把「上传大小上限」改成 200MB，页面显示保存成功，
实际上传永远被 10MB 拦死。这不是「没登记」，是**已登记、已暴露 UI、已落库，就是没人读**——比僵尸参数更隐蔽。

> **✅ 已于 2026-10-10 修复（阶段 0.5，commit 见 `git log --grep=MaxUploadSizeMB`）**：
> 上传链路改成 **env `UPLOAD_MAX_SIZE` > 库里的 `system_config.max_upload_size_mb` > 10MB 代码兜底**。
> 配套：`SystemConfigService.ResolveUploadMaxBytes` 只认"行真实存在且 > 0"的值（不走 `GetConfig`，
> 否则读失败时它的 `defaultConfig()` 会拿 50MB 顶上，等于把存量站点从 10MB 悄悄放宽 5 倍）；
> `systemConfigRepo.GetConfig` 加 nil 句柄哨兵错，避免上传链路因装配问题崩进程。
> **✅ `/api/upload` 这条链路的两份后端副本已收敛（阶段 0.5 → 阶段 2a）**。最终优先级链
> （`controller.resolveUploadMaxSize`，`upload.go:111`）：
> **env `UPLOAD_MAX_SIZE` > `misc.upload_max_size_mb`（参数中心，2a）>
> `system_config.max_upload_size_mb`（兼容列，0.5）> 10MB 代码兜底**。
> 关键取舍：`misc.upload_max_size_mb` **默认值取 0**，语义是「不覆盖，沿用下游」——
> 填 10 会把运维已在系统配置页调好的 200MB 又压回 10MB（把 0.5 刚修好的 bug 原样退回），
> 填 50 则让新装站点从 10MB 静默放宽 5 倍；取 0 让存量与新装站点行为与上线前逐字节一致。
> 该键每请求读，改完即时生效。
> **✅ 前端那份已于 2026-10-12 接上**（原记「→ 阶段 3.3」，本轮做完）：
> `MaterialLibrary.vue:329` 现在是 `rtNum('frontend_ui', 'material_max_size_mb', 10)`，
> 报错文案同步改成 `t('文件大小不能超过{size}MB', { size: maxSizeMB })`（9 份语言包的键一起从
> 「不能超过10MB」改成「不能超过{size}MB」）——**上限进了参数中心而提示还写死 10MB**，
> 等于运维调到 20MB 后页面继续告诉用户「不能超过 10MB」，那比不接更糟。
> 第三个实参是**字面量 10**（不是命名常量），所以这条在清单里仍算 `TUNABLE` 点位、
> 但在 G1 里算**前端读取点**（两个口径的差别见 §1.2）。
> **⚠️ 还有一份没接，而且是故意不接**：素材库的**后端**判断
> `internal/content/controller/material.go:139-140` 仍写死 `header.Size > 10*1024*1024`
> 与 `"文件大小不能超过10MB"`。它不属于上面那条优先级链（另一条上传链路、另一个切片）。
> 为什么不擅自接：把它接进 `resolveUploadMaxSize` 的同一套链，链上的**兼容列**默认是 50MB
> （`gorm:"default:50"`，存量行基本都带着这个值），于是这次接线会把一个线上端点的上限
> 从 10MB 静默放宽到 50MB —— 与 0.5 那次「不能让新装站点静默放宽 5 倍」是同一条红线。
> 要接得先拍板素材库这条链要不要跟随站级上传上限，属产品决策不是配置债。
> 现在两侧读数一致（前端参数默认 10、后端写死 10），**没有**「前端 50 后端 10」那种错位。
> **同类但没动的两处前端上限**（本次扫描顺带查出，列在这里不擅自改）：
> `user-web/src/views/KnowledgeWorkspace/KnowledgeManagement.vue:416`（50MB，:200 的提示同值）
> 与 `.../BatchImport.vue:219`（20MB，:50 的提示同值）。前者与后端
> `knowledge.max_upload_file_size`（兜底 50MB，`knowledge_base_import.go:36 DefaultMaxUploadFileSize`）
> 是**同一个概念的两份真相**：运维把该参数调到 100MB 后，前端仍会把 60MB 的文件拦在本地。
> 接它要么再造一个前端旋钮（两份真相，正是本任务要消灭的形状），
> 要么把 `knowledge` 组放进 `/api/config/runtime` 的公开白名单（那是对外暴露面，得单独拍板）。
> **⚠️ 别混淆**：`knowledge_base_import.go:36 DefaultMaxUploadFileSize = 50 << 20` 是**另一条**
> 知识库文档上传上限，已在阶段 1.8 作为 `knowledge.max_upload_file_size` 单独入库，与本节不是同一处。

同类：JSON body 上限 `middleware/body_limit.go:30 DefaultMaxJSONBodyMB=8`。

> **✅ 已于 2026-10-10 入库（阶段 2a）**：`middleware.max_json_body_mb`，默认 8、`Min 1`、`Restart true`。
> 三个决策：① 默认必须保持 8 —— `TestGlobalDefaultDoesNotTightenExistingCaps` 钉死
> 全局默认值 ≥ webhook 可调顶格 4MB，调小会把运维已放行的高段请求截断；
> ② **有意不给 0** —— env 侧 `<=0` 的「不限制」是部署层排障应急开关，参数中心是运维日常调的面，
> 再开一个关掉全局 body 封顶的口子风险大于收益，Description 里指明"临时放开请用 env 设 0"；
> ③ `Restart: true` —— 这是 `ParamDef.Restart` **首次被真正用上**（此前 115 条种子一条都没填），
> 因为 `router.Setup` 里 `r.Use(middleware.BodyLimit(...))` 是装配期读一次，不重启不生效。
> 另外 `maxMultipartMemoryMB` 保持独立不联动（它是把 gin 默认 32MB 主动调小的内存优化，
> 跟随 body 上限会顺带把每个并发上传的内存放大）。

**② Webhook 验签降级开关 —— ✅ 已处置（2026-10-10，阶段 2b），结论是「参数化不是答案」**

本清单原记录「`ALLOW_INSECURE_WEBHOOK` 14 处重复、默认 true」，**该描述经实测为假**，如实更正：

- 只有 3 个真实读取点，其余 11 处是错误文案/日志里提到变量名。
- 三处**本来就 fail-closed**（只认字面量 `"true"`，且仅限该账号压根没配密钥时才可能豁免），不存在"默认 true"。
- 诊断从"这是安全默认值写反了"修正为"三把开关的护栏强度不一致"，**真正的问题是强度不是默认值**。

| 变量 | 读取点 | 原护栏 |
| --- | --- | --- |
| `ALLOW_INSECURE_WEBHOOK` | `internal/service/webhook.go:509` | 启动护栏：非开发环境 `log.Fatalf` |
| `ALLOW_INSECURE_TELEGRAM_WEBHOOK` | `internal/channelbot/telegram/telegram.go:463` | **无任何环境护栏**（唯一缺陷） |
| `MARKETING_WEBHOOK_ALLOW_INSECURE` | `internal/content/service/marketing_flow_action.go:703` | 运行时判 `config.IsDevelopmentEnv()` |

顺带查出**第三个"似的缺陷"**：`internal/channelbot/whatsapp/whatsapp.go` 的 `VerifyWebhook` 在空 secret 时
直接返回 true（fail-open），无 env 开关、无护栏、无 warn 痕迹，注释还写着"与项目既有行为一致"。
它**不在硬编码盘点范围内**（不是字面量，是控制流写错），但性质更严重，已在本批一并改为 fail-closed。

**为什么最终不做成 DB 参数**（详见 `CONFIG_DB_DRIVEN_PLAN.md` §阶段二 2b）：做成数据库可改等于让
admin API 的写操作能在运行时翻转签名校验 —— 相比 env + 启动 `log.Fatalf` 是**安全回退**
（DB 改完即时生效、无重启、无启动拦截）。且 DB 镜像 env 状态必然造假值（ReadOnly 与"服务端自写"
自相矛盾，或静态 false 纯误导）。**判定标准：能不能让运行时的一个写操作翻掉安全闸** ——
凡能的一律留 env + 启动护栏，此为"全面数据库驱动"的受控例外。

实际落地 = 补齐 TG 开关的 dev 环境护栏 + whatsapp fail-open 改 fail-closed + 启动护栏扩到两个变量 +
测试逐格钉住只认字面量 `"true"`（`"TRUE"`/`"1"`/`"yes"`/`"true "` 都不开）。`wired` 计数不变（无参数键）。

**③ 分布式锁与幂等 TTL（收件箱/消息中台）**
`InboxLockTTL=24`、`InboxAILockTTL=15`、`InboxPendingTTL=5`、`IngestLockTTL=25`、
`InboxContentDedupTTL=5`、`InboxAIProcessingTTL=2`、`MessageHubDefaultIdemTTL=24`
（全部在 `internal/service/inbox_ingress.go` / `message_hub.go`）；
`HumanLockDefaultTTL=24`、`HumanLockReasonTTL=24`、`LockExpiryCheckInterval=time.Minute`
（`human_escalation.go`）；`PollingLockStaleThreshold=60`（`repository/telegram_polling_lock.go`）。
锁 TTL 写错 = 死锁或重复处理，不是「调优」而是「正确性」。

**③ 处置结果（2026-10-10，阶段二 2c）：新建 `lock` 组 7 条，13 个点位接得动 6 条**

| 点位 | 处置 |
| --- | --- |
| `InboxLockTTL=24` | ✅ `lock.inbox_human_ttl`。**顺带接上 `inbox_ingress.go:231` 一处裸 `24*time.Hour`**（人类锁 reason 键的 TTL，与锁同一把却走字面量），与锁共用同一参数 |
| `InboxPendingTTL=5` | ✅ `lock.inbox_pending_ttl`（生产调用方 `service/ai_debounce.go:68`） |
| `InboxContentDedupTTL=5` | ✅ `lock.inbox_content_dedup_ttl`（经既有 `contentDedupWindow()`，字段优先、零值回落） |
| `IngestLockTTL=25` | ✅ `lock.ingest_lock_ttl`（生产调用方 `inbox_ingress_persist.go:137`） |
| `InboxAIProcessingTTL=2` | ✅ `lock.ai_processing_ttl`（生产调用方 `:483`/`:585`，这是**真正在跑的防重复回复并发闸**） |
| `MessageHubDefaultIdemTTL=24` | ✅ `lock.message_hub_idem_ttl`，**本组唯一 `Restart=true`**：读取点在账号服务构造期，而账号服务有 6 处构造点全在启动时完成（`qq_account.go:146`、`wecom_integration.go:34`、`feishu.go:115/562/932`、`email.go:55/63`），运行时改值对已启动账号不生效 |
| `InboxAILockTTL=15` | ❌ **刻意不接**。唯一读取点 `inbox_ingress.go:265` 所在的 `tryAcquireAILock` 自带 `//nolint:unused //// 仅被 *_test.go 引用，生产路径未用`，`ReleaseAILock`/`IsSessionAIBusy` 同样无生产调用。保留「未接线」标注并在 Description 写明「真正在跑的并发闸是 `ai_processing_ttl` 那条」 |
| `PollingLockStaleThreshold=60` | ✅ **早在阶段 1.1 已接**（`misc.polling_lock_stale_threshold`），本批不重复 |
| `HumanLockDefaultTTL=24`、`HumanLockReasonTTL=24`、`LockExpiryCheckInterval=1m` | ❌ **不新增种子，只记死代码事实**。这三条常量所在的 `HumanEscalationManager` 在生产链路上死了：`NewHumanEscalationManager` 唯一生产引用为零（只有 `human_escalation_test.go` 19 处调用），`TriggerCensorshipEscalation`/`IsSessionLockedForHuman`/`StartLockExpiryChecker` 全树零生产调用。这是「运行时有同名锁、但实现不在这个文件」的典型——真正在跑的是 `inbox_ingress.go` 的 `InboxHumanLockKey` 系列（`hivemtk:lock:human:`）。给一条死代码建参数只会让人以为改它有用 |

**顺带修掉一个真 bug（与硬编码无关，是写对了才暴露的）**：`internal/cache/memory.go` 的 `PopAll` **不判过期**。`Get`/`Exists`/`LRange`/`scanExpired` 全都判 `item.expiration`，唯独 `PopAll` 只判 `listMode` 和 `len(listItems)`，所以 `LPush(key, v, 60ms)` 写入的列表过了 TTL 仍被原样 Pop 出来。影响面：只在内存缓存（`cache.NewMemoryCache()`，无 Redis 时的回退路径）上显形；Redis 路径由 `EXPIRE` 保证不受影响。对应业务：AI 防抖暂存的入站消息本该随 TTL 过期，内存回退下会被迟到的 Pop 重新投给 AI。修法是补一段与 `Get`/`Exists` 逐字同构的过期判断（`IsZero() && Before(time.Now())`），顺带把过期项从 LRU 摘掉。**这个 bug 是 `TestPendingMessagesExpireByConfiguredTTL` 首跑红暴露的**——不是因为接线写错，是「写对了才看得见底下有坑」。

**④ 渠道与配额硬上限**
`QQMessageMaxLen=2000`、`TGMessageMaxLength=4096`、`TGInlineRowsMax=100`、`TGInlineButtonsPerRowMax=8`、
`SSEMaxConnPerIP=5`、`TierMinRatePerMin=12.0`、`TierQuotaMarketing=1`、`TierQuotaUtility=4`、
`WeComErrorRateDegradeThreshold=0.3`、`WeComQuotaDegradeThreshold=0.9`、`DefaultMaxJSONBodyMB=8`、
`MaxUploadSize=10`、`SSOCookieTTL=5`、`DefaultVisitorTokenTTL=7`、`MaxRetry=3`（短信）、
`ConsecutiveFailureLimit=3`、`L1TTLHours=24`、`MaxTokens=500/800/2048`、`EditLockTTL=5`、
`DefaultAudienceLimit=200`、`MaxAudienceLimit=500`、`CircuitBreakerShadow=false`、
`LTCConfigCacheTTL=60`、`TranslationCacheTTLDefault=1`、`WebHook*` 限流四项。

> **✅ 已于 2026-10-10 部分入库（阶段 2d，接 10 条）**：新建 `channelbot`（5）+ `sse`（3）+ `bridge`（2）
> 三组种子，`wired 110→120`。已接：`QQMessageMaxLen`、`TGMessageMaxLength`、`TGInlineRowsMax`、
> `TGInlineButtonsPerRowMax`、`DefaultHTTPTimeout`、`SSEHeartbeatInterval`、`SSEMaxConnPerIP`、
> `SSEClientBufferSize`、`SSEMaxBacklogEvents`、`SSEBusBufferSize`。
> 其中 `DefaultMaxJSONBodyMB=8` 已在 2a 单独入库（见 ①），`MaxUploadSize=10` 已随 ① 收敛成代码兜底，
> `SSOCookieTTL=5` / `DefaultVisitorTokenTTL=7` / 短信 `MaxRetry=3` 已在 1.1 接为 `misc.sso_cookie_ttl`
> / `misc.visitor_token_ttl` / `misc.sms_max_retry`。
> **仍未接**：`TierMinRatePerMin`/`TierQuotaMarketing`/`TierQuotaUtility`（WhatsApp 分层配额，
> 属 P1 → 阶段三 3.1）、`WeComErrorRateDegradeThreshold`（见 ③，判定是写死三档，单一阈值表达不了）、
> `ConsecutiveFailureLimit`/`L1TTLHours`/`MaxTokens` 三处同名不同值（见 §2.2）、
> `EditLockTTL`/`DefaultAudienceLimit`/`MaxAudienceLimit`/`CircuitBreakerShadow`/`LTCConfigCacheTTL`/
> `TranslationCacheTTLDefault`/`WebHook*` 四项（P1 → 阶段三）。

### 2.2 同名不同值：现测 10 个键自相矛盾（下表 9 行，两行已按拆键方式退出该病征）

CSV 里同一 `group/key` 出现多个值，说明**同一个概念在不同地方各写各的**。这是硬编码最典型的病征：

| group/key | 出现过的值 | 说明 |
| --- | --- | --- |
| `misc/misc_max_tokens` | `500` / `800` / `2048` | 三个调用点各写各的（`session_assignment.go:242`、`controller/ai_agent.go:205`、`ai_agent_domain.go:105`） |
| `frontend_ws/max_reconnect_attempts_default` | `10` / `50` | `agentSocket.js` 10 次，`chatSocket.js` 50 次。**已不属本病征**：接线时拆成 `agent_max_reconnect_attempts` / `chat_max_reconnect_attempts` 两个键，两个值各有自己的键（拆的理由见 §2.3 首行） |
| `frontend_ws/initial_reconnect_delay_ms` | `1000` / `2000` | 同上，同款拆法（`agent_` / `chat_` 前缀） |
| `knowledge/top_k` | `5` / `50` | 工作台 5、商户 playground 50 |
| `misc/approval_shadow` | `true` / `false` | `approval_wiring.go:243 false` vs `:261 true` —— **同文件内自相矛盾** |
| `agent_llm/{empathy,enthusiasm,patience,clarity,politeness}` | `3`/`4`/`5` | 5 档评分映射表（alignment_stage.go），是 1 个表不是 5 个参数 |
| `knowledge/similarity_threshold` | `0` / `1` | `knowledge_merchant_playground.go:69,72`，真假阳性 |
| `sales/collection` | `true` / `false` | 真假阳性（`ltc_config.go:83,856` 是字段赋值） |
| `frontend_ui/i` | `0`/`1`/`6`/`29` | 循环下标，真假阳性 |

治理动作：`max_tokens`、`reconnect` 系列应**合并成 1 个键**；
alignment 五维应合并成 1 张 `alignment_dimension_weight` 字典项，不拆成 5 个标量。

> **2026-10-12 复核（终态读数）**：现算是 **10 个键**同名不同值，不再是 12。
> 判据写清楚，否则这个数没人能复算：取 CSV 里 `kind=CONFIG` 且
> `bucket ∈ {TUNABLE, WIRED, PARAM_FALLBACK}`（Go 侧 378 行）∪ 前端全部 `CONFIG`（67 行）= 445 行，
> 按 `(suggest_group, suggest_key)` 聚合 `value` 集合，**且要求 `suggest_key` 非空**，
> 筛 `len(values) > 1` ⇒ 恰 10 个键：`misc_max_tokens`(3 值)、`misc_approval_shadow`、
> `knowledge_top_k`、`knowledge_similarity_threshold`、`sales_collection`、`frontend_ui_i`、
> `agent_llm_{empathy,enthusiasm,patience,clarity}`。
> 两个"为什么少了 2 个"都要说清，别让人以为是值收敛了：
> ① `frontend_ws` 的重连次数/首次延迟那两条**不再同名**——接线时按 §2.3 拆成
> `agent_*` 与 `chat_*` 四个独立键（种子第 732–741 行），"同一个键两个值"这个病征被
> "两个键各表其值"替换，这是治理动作的结果，值本身一个都没变；
> ② 复现这条判据时**必须显式要求键非空**：`WIRED`/`PARAM_FALLBACK` 那 137 行的
> `suggest_key` 是空的（清单只给待接点位建议键名），不过滤就会让每个 group 的空键聚成
> 一个假"多值键"——本轮第一次算这个数时就因为漏了这层过滤读出 **25**，其中 13 个是
> 这种伪键。写在这里是为了让下一个复算的人不再踩。
> 另：`agent_llm` 五维里现在只有 **4** 维多值（`politeness` 只剩一个值），
> 表头那行仍写五维是历史描述，别按它去找第五处冲突。

### 2.3 前端阈值（起点保守记 15 个真点位；下表 13 行覆盖 12 个已接常量名 + 3 个未接点位）

> 本表 2026-10-12 按磁盘现值重排：行号全部重新定位，「建议 group/key」一列换成了
> **实际登记的 key**（两者不同处已注明），并加了「状态」列——原表只列点位不列接线状态，
> 于是台账把 12 条已接的继续记成欠账。

| 常量 | 值 | 位置（现行号） | 实际 key | 状态 |
| --- | --- | --- | --- | --- |
| `MAX_RECONNECT_ATTEMPTS_DEFAULT` | `10` / `50` | `user-web/src/utils/agentSocket.js:11`、`chatSocket.js:9` | `frontend_ws.agent_max_reconnect_attempts` / `chat_max_reconnect_attempts` | 已接（**拆成两个键**：两处默认值本就不同，合并成一个键必然让其中一个说谎） |
| `INITIAL_RECONNECT_DELAY_MS` | `2000` / `1000` | 同上 `:12` / `:6` | `frontend_ws.agent_initial_reconnect_delay_ms` / `chat_initial_reconnect_delay_ms` | 已接（同款拆法） |
| `MAX_RECONNECT_DELAY_MS` | `30000` | 同上 `:13` / `:5` | `frontend_ws.max_reconnect_delay_ms` | 已接（两个文件**共用一个键**：值同为 30000，且退避上限必须是同一个数） |
| `PING_INTERVAL_MS` | `25000` | 同上 `:14` / `:7` | `frontend_ws.agent_ping_interval_ms` / `chat_ping_interval_ms` | 已接 |
| `ACK_BATCH_INTERVAL_MS` | `200` | 同上 `:15` / `:8` | `frontend_ws.agent_ack_batch_interval_ms` / `chat_ack_batch_interval_ms` | 已接 |
| 访客轨迹历史 / 队列 / 刷报间隔 | — | 未接入的 `journeyTracker.js` 已删除（2026-11-26） | `frontend_ws.journey_history_max` / `journey_queue_max` / `journey_flush_interval_ms` | 已下架：无生产入口与读取点 |
| `COUNTS_POLL_MS` | `30000` | `views/approvalTask/List.vue:371` | `frontend_ui.approval_counts_poll_ms` | 已接 |
| `STATS_POLL_MS` | `30000` | `views/badCase/List.vue:551` | `frontend_ui.bad_case_stats_poll_ms` | 已接 |
| `REFRESH_MS` | `30000` | `views/messageHub/Dashboard.vue:112`（`:273` 的 `setInterval(load, REFRESH_MS)` 直用常量） | — | **未接**（`TUNE`/待接候选；与上面两条同值但不同语义，没并键） |
| `POLL_FAILURE_LIMIT` | `3` | `views/browserAutomation/Monitor.vue:287` | `frontend_ui.browser_poll_failure_limit` | 已接 |
| `DEEP_LINK_SCAN_PAGE_SIZE` | `500` | `views/customerSession/List.vue:455` | `frontend_ui.deep_link_scan_page_size` | 已接。原表建议归 `pagination.page_max_size`，**那条已下架且零读取点**（§7.2），把前端扫页大小挂到一个没人读的键上等于再造一次假接线，所以登记成 `frontend_ui` 专用键 |
| `WINDOW_HOURS` | `1` | `views/messageHub/Dashboard.vue:111` | — | **未接**（清单里是 P1） |
| `maxSize` → 现为 `maxSizeMB` | 兜底字面量 `10` | `views/system/MaterialLibrary.vue:329` | `frontend_ui.material_max_size_mb` | 读取点已接、**常量名没接**（第三个实参是字面量，仍算 `TUNABLE` 点位，见 §2.1① 与后端那份 10MB） |

**12 个常量名坐在 `rt*` 兜底位 = 清单的 17 行 `WIRED`；17 个 `group.key` 坐标 = G1 的
「前端真实读取点 17」**——两个 17 不是同一个数，前者是常量声明行、后者含 `material_max_size_mb`
这种字面量兜底的读取点，别把它们当成一处对账（`agentSocket.js`/`chatSocket.js` 各 6 处调用、
`journeyTracker.js` 3 处、五个视图各 1 处 = 20 处调用折成 17 个坐标）。

上述计数为 2026-10-12 的扫描快照；2026-11-26 删除未接入的旅程追踪模块后，其三个读取点已移除、参数已下架。现状以 `scripts/check-config-param-readpoints.py` 输出为准。

前端自己发明的默认分页/时间窗另有 **1754 处**（`TUNE` 桶），集中在 `src/api/*.js`
（`limit=20/50`、`days=7/30`）与 `views/**`（`pageSize||20`）。这些**不是常量声明**，
扫描器只能给出候选。它们绕过了已有的 `pagination` group 参数，属于"两边各有一个真相"，
建议在接线 `pagination` 组时**顺手把前端这批默认值改为从后端参数取**（见方案 §5 阶段三）。

---

## 3. F 桶：38 个业务字典（不进 `config_params`）

`config_params` 是**标量 KV**，塞不进「有序阶段 + 多语言标签 + 状态机」这种结构。
这 38 项需要独立的**字典表模型**，硬塞进参数中心会把 `param_value` 变成第二份 JSON 泥潭：

- 状态机/值域类（18）：`ApprovalStatuses`、`ApprovalDecidedStatuses`、`BadCaseStatuses`、
  `BillStatuses`、`BillStatusesChased`、`HumanTaskStatuses`/`Open`/`Terminal`、`KBChangeStatuses`、
  `OpportunityStages/Statuses/Outcomes`、`OrderDraftTerminalStatuses`、`PaymentStatuses`、
  `PaymentStatusesCounted`、`QuoteStatuses`、`KnownRiskLevels`、`AllStages`
- 分类标签类（11）：`BadCaseSources/Labels/FixLayers`、`HumanTaskKinds/Actions`、
  `KBChangeOps/AuditActions`、`AllKeyIntents`、`AgentStageNames`、`AllHumanizeDimensions`、
  `AllSalesChampionDimensions`
- 流程步骤/键名清单类（4）：`DefaultPipelineSteps`、`DefaultSendPipelineSteps`、`LTCKnownStages`、
  `LTCKnownThresholdKeys`
- 意图/提示词类（3）：`DefaultIntents`、`SystemPromptTemplates`、`DefaultLongtailTemplates`
- 其它（2）：`HumanTaskSLAColumns`（真·DB 列名，属 MODEL 不属配置，**不迁**）、`SystemRoleList`

**本节数目从 44 改到 38，是扫描器缺陷，不是治理进展**（明细见 §1.2 对账段的括注）：
`hardcode_inventory.py` 的 DATA 收集只按「名字里带不带字典型词缀」筛，
于是 `dialog_manager.go:363,366,369,372,375,378` 的 `intent.Categories = []string{"social"}`
这类**结构体字段赋值**（字段名恰好叫 `Categories`）也被当成业务字典记了 6 行。
判据改成「等号左侧必须是裸标识名（可带 `var`/`const`）且与扫描器给出的常量名一致」后，
实测恰好只少这 6 行。这类桶的边界由一张词缀表决定，**词缀表天生追不上命名**，
所以每次动它都要把「新命中/新丢弃哪些行」列出来逐行核，别只加词。

**与上一版台账相比，三处名字级移动**（都可以用 §0 的命令复现）：
`AllPersonaDimensions` 随 `persona_evaluator.go` 整体删除（`9a5f716c`）而消失；
`IntentEnabled` 现在是单个布尔开关、被归类成 `TUNABLE` 而不是字典；
`LTCKnownThresholdKeys` 是本轮补回的那个（词缀表此前没有 `Keys`）。

前端对应物在 `user-web/src/constants/`：现测 **22 个字典文件**（外加 `index.js` 汇总件），
`index.js` 对其中 21 个做了「双导出」（`export * as x from './x'` ＋ `export { default as xDefault }`）——
**`industry.js` 是唯一漏网的那个**，两个视图（`assetMarket/MyAssets.vue`、`assetMarket/Market.vue`）
是直接按路径 import 它的，所以不影响功能，只说明那张汇总表没跟住目录。
更要紧的一条：**`index.js` 自己零消费者**（全仓没有任何 `from '@/constants'`，也没有
动态 `import()` 或 `import.meta.glob` 指向这个目录——`import.meta.glob` 只用在 `router/modules`），
所以「由 `index.js` 双导出统一」目前只是文件里的意图，不是运行时的形状；
顺着这条链还带出 **5 个零消费者的字典文件**（`aiAgentType`、`assetType`、`intentType`、`priority`、`riskLevel`，
25 个导出符号在 `user-web/src` 全树命中数都是 0）。**已登记未处置**（见 §8）：
删前端文件要过 UI 泳道的浏览器回归，且这是功能债不是配置债。

**前后端各有一套状态字典，这是最容易出线上不一致的地方**，值得单独立项（见方案 §5 阶段四）。

---

## 4. C/G 桶：中文文案不是"配置"

12534（Go，2026-10-13 `15ccabb7` 影子面产物；上一批 `f7381630` 面是 12533，2026-10-12 那趟是 12505）
＋ 7348（web）条中文字符串，
二次切分后（子桶规则就是 `hardcode_inventory.py`
里的 `cn_bucket()`，下表用同一个函数对现产物跑出来的）：

| 子桶 | Go（现测） | web（现测，进 CSV） | 归属 |
| --- | --- | --- | --- |
| `LOG`（日志/注释，`logger.*`、注释行） | 3131 | — | ❌ 不治理（日志是自己看的） |
| `TOOL_SEED_COPY`（`cmd/` 工具种子文案） | 199 | — | ❌ 不治理（离线工具的素材） |
| `API_MESSAGE`（经 API 返回给用户的中文） | 9051 | 7322 | ✅ 走 i18n 词条 |
| `AI_OUTPUT_REPLY`（AI 回复模板/建议回复） | 153 | 26 | ✅ 走 i18n + 可选 DB 化 |

三处口径要点，别把这张表读成「Go 的文案也进了清单」：
① **CSV 只收 web 侧 CN**（`rows_from_web()`），Go 侧那 12533 条只在
`hardcode-sweep.json` 的 `cn_strings` 里，复现这张表要给 `cn_bucket()` 喂 Go 产物；
② `TOOL_SEED_COPY` 在进 CSV 前被显式跳过，所以 web 的 7348 = 7322 + 26；
③ 首扫那张表里还有一行 `SQL_DEBUG 4`——`cn_bucket()` 里**没有这一支**，
它是当时手工归类的产物，本轮起不再出现在任何读数里。

**重要区分**：文案缺口是 **i18n 词条**问题，不是"数据库配置"问题。
前端已有完整 vue-i18n（`src/i18n/`，9 份语言 802 KB，2119 处 `t()`/`$t()` 调用、
G6 现测去重键 923 个），后端已有 `internal/pkg/i18n/`（G6 现测 msgid 157 条）。
机制都在，缺的是把 `API_MESSAGE` 那一万条搬进词条表。
**不要把文案塞进 `config_params`**——
那会让参数中心同时承担数值阈值和多语言文案两种完全不同的生命周期。

---

## 5. 不迁项（写明理由，避免下轮又被当成漏项）

- **B 桶 PROTOCOL（51，2026-10-12 终态；本轮起从 20 涨到 51 是"看得见了"，不是新增契约）**：
  `domain/errors/codes.go` 12 个错误码、`channelgw` 协议版本 v1/v2、
  `traceparent` 的 TraceID/SpanID 十六进制长度、RFM 的 `RDays1=7`——这 20 条是原表就记着的；
  本轮补齐常量声明面后又进来 **31 条带类型标注的枚举成员**（`clue.go:84-102` 的 19 个 `ClueType*`、
  `type/type.go:14-33` 的 `UserStatus*`/`OrderStatus*`/`AccountStatus*` 7 条、
  `inference_types.go:112-115` 的 `Crisis*` 4 条、`quote.go:188 QuoteVersionFirst`；
  另有 `type.go` 的 4 个 `SmlistType*` 成员名不匹配任何词缀表，落进了 `FIELD_LIT`）。
  这些是**对外契约**，改一个字就是破坏兼容。归 `PROTOCOL`，永不进配置。
  顺带查出**一处契约内部的取值冲突，而且它已经在产生错数据**（详见 §8 表末行，本批只做了行为不变的那半步）：
  `clue.go:95 ClueTypeGeoCapture = 9` 与 `:96 ClueTypeDouyin = 9` **同值**，
  而权威字典 `defaultClueTypes`（`clue.go:164`）只把 `"9"` 标成「抖音」——
  GEO 决策链捕获的线索（`router/geo_routes.go` 的工作流捕获执行器写入）因此在列表页显示成「抖音」、
  评分走 `clue_score.go:336` 的抖音档（65）。
- **C 桶 DEPLOY（13）**：`config/ports.go` 6 个默认端口、`pkg/db/db.go` 的 `MaxOpenConns=20`、
  `pkg/mail` 的 SMTP 端口/SSL、`testdb` 的 `POSTGRES_TEST_PORT`。这些属于**部署期 env**，
  改要走 `check-env-coverage.py` 那条线，不是 `config_params`。
- **D 桶 INFRA（9，本轮 +1）**：`BGEM3DefaultDimension=1024`、`SourceDim=16384`、`HNSWM=16`、
  `HNSWEfConstruction=200`、`HNSWEfSearch=100`，加上新看见的
  `pgvector_index_ops.go:15 HNSWMinRowsForIndex int64 = 50000`（低于该行数不建 HNSW 索引）。
  **能配，但改了要重建向量索引**。
  建议做成"可配 + 变更需审批 + 写审计"，`Restart=true`。
- **E 桶 FIELD_LIT（459，2026-10-12 终态；本轮记过 455）**：`InboxCreated = true`、`FromCache = true`、`OK = true`、
  `Processed = true`、`Delivered = 0`。扫描器按 `Ident = 布尔/数字` 抓到的一切赋值，
  其中绝大多数是**业务数据字段的字面量**，不是配置常量。

---

## 6. H 桶 INLINE_NUM（1459，2026-10-13 影子面产物）：本轮结论是"不批量治理"

抽样归类：`os.Exit(1)`、`blockedCount.Add(1)`、`atomic.Store(0)`、
`Order("id DESC").Limit(3)` —— **1459 条里按 G4 那张阈值词缀表（`Threshold|Limit|Max|Min|TTL|
Timeout|Interval|Window|Size|Count|Concurrency|Retries|Rate|Weight|Percent|Seconds|Days|Hours|
Cap|Ceiling|Floor|Batch`，忽略大小写，匹配整行上下文）命中 150 条，其中大部分还是计数器初值**
（`blockedCount.Add(1)`、`windowUsed.Store(0)` 这类）。把计数器抽成配置既无意义也无害处可言。
**本轮不为 INLINE_NUM 立批量工单**；如后续真有"某个 Limit 写死"的 bug，走单点修复。
那 150 里有一条是词缀表自己的假阳性可以作证（`geo/model/push_record.go:21` 的
`BatchID string` 是字段名撞上 `Batch`），**这说明"整行匹配词缀"只能用来估量级，不能用来出工单名单**。

**但"不治理"不等于"里面没有真 bug"，这条边界要写清**：本轮就是在这一桶里撞见一处已经在产生
错数据的裸字面量（`router/geo_routes.go` 给 GEO 捕获线索写 `Type: int64(9)`，而 9 是抖音的码，
详见 §5 B 桶与 §8 表末行）。它是 INLINE_NUM 里**唯一**被单点处理掉的一条——
判据不是"这个数字看着像阈值"，而是"它的值与另一处命名常量冲突且已经在写库"。

**与 G4 声明面的关系（别把两个盲区数混成一个）**：G4 只卡**命名常量**的新增，
`scripts/measure-tunable-blindspot.py`（只读物，不进门）现测同一口径下的覆盖面：
扫描面 1763 个 Go 文件 / 包级阈值命名数值常量 **475** 个 / 其中 G4 声明面可见 **197** 个 /
盲区（名字小写）**277** 个 / 盲区（名字大写却看不见＝值形状未覆盖）**1** 个
（`pkg/utils/type/type.go: OrderStatusTimeout OrderStatusType = -1`，哨兵值不是旋钮，故意不补形状）。
（测于 §7.1 注里那棵"能提交的树"：tip `15ccabb7` 影子树＋本泳道 45 个文件，2026-10-13 08:36 现跑；
同一枚脚本在工作树上跑**读数逐字相同**（`1763 / 475 / 197 / 277 / 1`，08:21 现测）。
这枚与 G1/G7 的枚举源不同：它走 `Path.rglob("*.go")` 现读文件系统（脚本 :103），不是 `git ls-files`，
所以未跟踪件**会**进它的视野——这次两边相同，是因为并行泳道现在那三枚未跟踪件
（`user-server/docs/capabilities.md`、`user-server/internal/bridge/testdb_helper_test.go`、
`user-server/internal/service/menu_ceiling_test.go`）里两枚是 `_test.go`（被它的排测试规则滤掉）、
一枚根本不是 `.go`，三个都不进它的面。
注意它与 §1.2 的产物面不是同一口径：产物用的 `hardcode_sweep.py` 现测 **1758** 个文件，
差的这 5 个是这枚只读物**不排**种子件——把两边的成员集合现取出来做差，多出来的正是
`cmd/seedkw/main.go`（被 sweep 的 `cmd/seed` 段挡掉）、`internal/geo/service/keyword_seeds.go`、
`internal/service/config_param_group_seeds.go`、`internal/service/config_param_seeds.go`、
`internal/service/dictionary_seeds.go`（后四条被 sweep 的 `*_seeds.go` 后缀挡掉）；
反向差集为空（sweep 没有 blindspot 看不到的文件）。
先前版本在这里记的是 `1759`／`1762`，那是 tip 动过、数没跟着重取留下的旧值。）
剩下那 1459 条内联数字与这 277 条小写常量都在 G4 视野之外，
这是"按名字判"这条设计代价的具体形状，**报"新增未入库=0"时不许顺带说"仓里没有未接线的阈值"**。

---

## 7. 72 条僵尸参数：已登记但没人读（横切本清单的第一优先）

`scripts/check-config-param-readpoints.py` 在**阶段一开工前（2026-10-09）**实测：
**115 条登记 / 43 条真接线 / 72 条僵尸（63%）**。
下面这张表和这组数是**历史基线快照**，本轮不再复算它们；当前读数只在 §7.1 那张门表里记
（`204 有读取点 / 217 种子 / 13 已下架 / 僵尸 0`）。

这 72 条**不是"没做"，是"做了一半"**：参数已在库、已在「参数中心」页面渲染成可编辑控件、
运维改完看到「保存成功」——但生产代码从来不读它。这是对运维的**主动误导**，比没做更糟。

按 group 分布（`group / key / 名称`）：

> **处置进度（2026-10-10）：阶段一全部 8 批 + 阶段二 2a/2b/2c/2d + 阶段三 3.1 第一批已接线并提交。**
> 门禁读数：`wired 40→198`、`UNDECLARED 72→0`、`已声明未接线 14`。（**都是当时那版门的读数**，
> 与上面 115/43/72 那句里的 43 差 3 条；那 3 条的判据本轮无法复现——那版 `readpoints` 已被重写两次，
> 所以两处读数各按原样保留、不互相推算。当前终态读数只看 §7.1 那张表。）
> 阶段一 8 批（wired 40→102）处理的是本节的僵尸参数；阶段二起处理 §2.1 的 P0 新增点位，
> 阶段 3.1 第二批（wired 133→140）接的是 webhook 回调配发七件套（dedup_ttl / rate_limit /
> rate_burst / max_retries / worker_count / queue_size / reply_concurrency）。
> 阶段 3.1 第三批（wired 140→146）接的是 SOP 调度器并发三项 + Saga 补偿三项。
> 阶段 3.1 第四批（wired 146→157）接的是域名健康度阈值 4 条 + RAG 健康度窗口与六维权重 7 条
> （六项权重按总和归一化，否则运营调大一项就会让总分超出 0-100、等级分档错位）。
> 每接一条也会让 wired 读数上涨。2a 接的 `misc.upload_max_size_mb` / `middleware.max_json_body_mb`
> 即对应 §2.1①；2c 是新登记的 `lock` 组 7 条（含 1 条刻意不接，故 wired +6 而
> 「已声明未接线」从 13 涨到 14）；2d 是新登记的 `channelbot`/`sse`/`bridge` 三组 10 条，对应 §2.1④；
> 阶段三 3.1 第一批 9 条（`confidence` 4 + `sales` 2 + `misc` 1 + `memory` 1 + `geo` 1）
> 对应 §2.1④ 与 §2.2 的 P1 未接项，其中 `geo.default_visibility_days` 拆开了
> `q.Days <= 0 || q.Days > 365` 那一个条件兼任的两件事。
> 下表是**接线前**的基线快照，各行状态见行末标注。

### 7.1 阶段五：七道门已落地（G1–G6 于 2026-10-11，G7 于 2026-10-12）

阶段一到阶段四把"能接的都接了"，但那是一次性动作——**下一次有人加新的硬编码，
没有任何一道门会问他要不要入库**。阶段五把方案 §6 的六道门全部落地，接进
`make audit` 与 `.github/workflows/user-server-ci.yml` 的 `static-gates` 作业。
G7 是本轮（阶段六收尾之后）加的：改造过程中在两个文件里发现**注释已经被编辑工具写坏了**，
坏法不是"少个字"而是"变成合法的乱码"，编译与既有门都读得出来才怪——于是把那一次性
的手工排查换成每次提交都跑的判据（见下表 G7 行与方案 §6.1）。

> 下表是 **2026-10-13 现测**读数（逐条 `python3 scripts/<门>` 跑出来的输出行，不是引用上一轮的记录）。
> 每次跑门都会重新打印这些数，怀疑台账过期就按 §0 那八行复跑，别信这里的日期。
> **扫描面那几格记的是"能提交的那棵树"的读数**：tip `15ccabb7` 的 `git clone --shared` 影子树里
> 只叠上本泳道的 45 个改动文件（`/tmp/r83-shadow/hivemtk`，08:35–08:36 现跑，
> 取证日志 `/tmp/r81_final/fp_shadow4/<腿名>.log`，每腿一份）。
> 这一轮的每一腿输出都带 `### TREE=` / `### LEG=` / `### NOW=` 三行抬头，
> 所以两个面的读数不会认错归属——上一版就吃过认错的亏，见本节末。
> 同一批门在**工作树**上跑（同 tip，`git status --porcelain` 09:00 现测 189 M ＋ 8 未跟踪——
> 本泳道 40 M ＋ 5 未跟踪，其余 149 M ＋ 3 未跟踪是并行泳道的在途件；门跑在 08:21:26–08:33，
> 日志 `/tmp/r81_final/fp_work2/`）与影子面 `diff` 对过，只有一格不同：
> G7 的面数 `7245 路径 / 7226 文本`（影子）vs 工作树 `7247 / 7228`（08:57）→ `7248 / 7229`（09:01）。
> 差的正是那几枚未跟踪件，且**这一格几分钟就在动**：08:58 那趟 `comm` 出 2 个
> （`user-server/docs/capabilities.md`、`user-server/internal/bridge/testdb_helper_test.go`），
> 09:01 那趟多出一枚 `user-server/internal/service/menu_ceiling_test.go` 变 3 个
> （两份 `git ls-files --cached --others --exclude-standard` 名单现取后 `comm` 出来的，反向差集为空）——
> 所以这两个数只能连时间戳一起引，单引一个数就是引一个已经过期的快照。
> G1／G3 两侧**连扫描面都逐字相同**（`Go 1867 / Get* 232`、`Go 1768 / 常量池 2822`）。
> **两棵树的读数为什么"只差一格"，判据是各门的枚举源，不是"工作树比影子多文件"这句笼统话**
> （枚举源逐条读脚本现取）：
>
> | 门／读物 | 枚举源 | 现测位置 | 未跟踪件进不进它的眼 |
> | --- | --- | --- | --- |
> | G1 `check-config-param-readpoints.py` | `Path.rglob` 走文件系统 | 脚本 `:123`、`:192` | 进（但排 `_test.go`） |
> | G3 `check-config-param-defaults.py` | `Path.rglob` 走文件系统 | 脚本 `:310` | 进（但排 `_test.go`） |
> | G7 `check-text-encoding.py` | `git ls-files --cached --others --exclude-standard -z` | 脚本 `:34`（口径写在文件头）、`:103`（实际调用） | **全进**，不排测试 |
> | `measure-tunable-blindspot.py` | `Path.rglob` 走文件系统 | 脚本 `:103` | 进（但排 `_test.go`） |
>
> 这轮那 2 个未跟踪件一个不是 `.go`、一个是 `_test.go`，于是只有 G7 那一格动，
> `rglob` 那三枚读数一字未变。**判"某枚门不随工作树漂"要先认它的枚举源**——
> 本节先前把 G1／G3 说成"按 `git ls-files` 枚举、所以未跟踪件进不了它的眼"，那是错的：
> 它们走文件系统，读出现场里"未跟踪件"照样被扫；两边读数相同只是因为那 2 个文件被它们自己的
> 排测试／非 Go 规则滤掉了。**机制要从枚举源读出来，不能从一次相等的读数反推。**
> **判据行两侧逐字节相同**（wired 204、四向 184+3+17+13=217、常量声明 294、NOT_UTF8/FFFD/MOJIBAKE/STALE 全 0）。
> **面数随 tip 漂，不是随树漂**（同一影子装配法，只换检出的 tip；四元组＝G1 Go／G3 Go／G3 常量池／G7 路径·文本）：
> `8a6d0c03` `1860 / 1761 / 2819 / 7224·7205` →
> `04dfb079` `1863 / 1764 / 2821 / 7229·7210`（并进来 14 个新文件，涨 3·3·2·5）→
> `f7381630` `1866 / 1767 / 2822 / 7234·7215` →
> `298da87b`＋`957af95c` `1867 / 1768 / 2822 / 7236·7217`（两笔各加 1 个 Go 文件与文案，
> 面数只在 957af95c 上现测过一次）→
> `15ccabb7` `1867 / 1768 / 2822 / 7245·7226`（Go 面不动，G7 那 9 格是他们入库的
> `MenuRouteAudit` 证据件：1 份 `.log` ＋ 8 份 `.tsv`）。**只对账判据读数，
> 看到面数与本机不符不是回归。**
> **本节先前记录的一处归属错误（留着，它是"两组数不等于两棵树"的现形教训）**：
> 有一版把 `1863…7229·7210` 记成影子面、把 `1866…7234·7215` 记成**工作树**面，
> 读起来像"影子 vs 工作树"的差。当时在 `04dfb079` 那棵影子树里复跑 G1/G3/G7，
> 输出正是 `1863 / 1764 / 2821 / 7229·7210`——证明**两组都是影子面，差在 tip 不在树**。
> **本轮又犯了一次同形的错，一并留着**：写上面那段对照时，我先给"工作树比影子多 2 格"编了一句
> "同路径同尺寸"的机制，又把 G1／G3 说成"按 `git ls-files` 枚举、未跟踪件进不了它的眼"——
> 两句都是猜的。逐条读枚举源才发现真相相反：G1／G3 走 `Path.rglob`（文件系统），
> 未跟踪件**照样进它们的眼**，两边读数相同只是因为本轮那两个未跟踪件被它们自己的
> 排 `_test.go`／非 Go 规则滤掉了；而唯一会随未跟踪件漂的是 G7（`--others` 那一路）。
> 猜测的机制一律不入库，机制要从枚举源读出来。

| 门 | 挡住什么 | 现测读数（2026-10-13，树与口径见上注） |
| --- | --- | --- |
| G1+G2 `check-config-param-readpoints.py`（改造） | 登记了没人读（僵尸）/ 接了线却不撤「未接线」标注 / **代码在读而种子里没有的孤儿 key** / 种子里没有的组名 | 扫描面 Go 文件 1867 / `Get*` 行 232 / 种子 217 条 / 前端消费组 3（排除 2 键）/ **前端真实读取点 17 个 group.key**；wired **204** / 已下架 **13**（接上却没摘标记 0）/ **UNDECLARED 0 / STALE 0 / ORPHAN 0 / 已声明未接线 0** |
| G3 `check-config-param-defaults.py` | 库里改了默认值、代码兜底常量还是老值；种子自己越界 | 扫描面 种子 217 / Go 文件 1768 / 常量池 2822（同名歧义剔除 209）/ `Default*Config` 字段 164 / 定位到兜底点的键 187；四向对账 **可比对 184 + 无法比对 3 + 前端下发 17 + 已下架 13 = 217**；**不一致 0 / 种子越界 0**；覆盖率棘轮 `scripts/config-param-defaults.baseline` = **184** |
| G4 `check-hardcode-tunable.py` | 新加的阈值/TTL/上限常量绕过参数中心 | Go 常量声明 **294** / 基线 TUNABLE **326**；**新增未入库 0** / 已作参数中心兜底 3 / 豁免（协议类＋`Default*` 兜底前缀）52 |
| G5 `check-config-param-keystyle.py` | key 命名不规范 / 组内重名 / runtime 白名单指向不存在的组 | 种子 217 条 / group 35 / runtime 白名单组 3；**命名违规 0 / 重复词 0 / 重复条目 0 / 幽灵组 0** |
| G6 `check-i18n-coverage.py` | 中文文案硬编码的覆盖率退步 | 见下 |
| G7 `check-text-encoding.py`（2026-10-12 新增） | 文本文件里的非 UTF-8 字节 / `U+FFFD` 替身字符 / 双重编码签名（UTF-8 被当 Latin-1 读后另存——字节合法、人眼可读、编译不报错） | 扫描面 git 列出 7245 路径 / 文本 7226 / 二进制跳过 19 / 读不到跳过 0（影子面；工作树同 tip 在 08:57 是 `7247 / 7228`、09:01 是 `7248 / 7229`，多出来的全是并行泳道的在途未跟踪件，见上注的枚举源表与那趟 `comm`）；**NOT_UTF8 0 / FFFD 超出基线 0 / MOJIBAKE 0 / STALE 0**；含替身字符的文件 1 个（`strings.ToValidUTF8` 的刻意用法，登记在 `scripts/text-encoding.baseline` = 1 处）。耗时（两侧各测两次）：user CPU **1.62–1.69s**；wall 随页缓存与并发摆动，本轮记到 1.95s（影子热）与 6.17s（影子冷、同机在跑四包 `go test`），工作树 2.02s／4.63s——**wall 不是可比的口径，用 user CPU**。**牙齿常驻**：`scripts/check-text-encoding.test.sh` 21 枚控制（假 git 树夹具，不碰工作树），已挂在同一作业的下一步 |

**总读数（阶段一到阶段六累计，2026-10-12 复扫确认，2026-10-13 于 `15ccabb7` 影子树再复跑）**：种子 **115 → 217 条**、`wired 40 → 204`、
`UNDECLARED 72 → 0`、`STALE 0`、`已声明未接线 14 → 0`（**13 条**改为**下架**并逐条写明为何接不上，见 §7 表格行末标注与 §7.2）。
10-13 这一趟判据行与 10-12 逐字相同；两棵树之间判据行同样逐字相同，**唯一会动的格是 G7 的扫描面**
（它的枚举源带 `--others`，未跟踪件直接进面，见 §7.1 注里的枚举源表），`rglob` 那三门（G1／G3／盲区）
两边同值。

**wired 这一格为什么从 206 掉到 204**：不是有人拆了两条接线，是 G1 的前端判据本身被修严了。
原实现把「`group.key` 属于 `runtimePublicGroups/Keys` 白名单」当成前端读取点，
于是 `frontend_ui.websocket_reconnect_max_wait_ms` 与 `frontend_ui.toast_duration_ms`
两条**前端全树零读取点**的键被算进 wired。改为只认代码里真出现的 `rt*(group, key, fallback)`
调用之后，这两条从 wired 移出、并按 §7.2 的口径登记为**已下架**。G3 的「前端下发」随之从 19
落到 17 是**另一条机制**：那两条键现在带 `deprecated=true`，在 G3 的四向分诊里落进「已下架」
而不是「前端下发」，所以是 `前端下发 −2 / 已下架 +2`，合计仍是 217。白名单在 G1 里换了角色：
不再当「前端在读」的证据，只用于另一端对账（服务端愿意下发而前端不读的键）。
留这个数在这里，是因为「白名单里有一条 = 有人读它」这类判据错位会把门变成橡皮章，
而 wired 读数上涨时没有任何一方会来问一句。

**清单口径的一处修正（2026-10-11 终验时发现）**：`hardcode_inventory.py` 原先把「已接进参数中心的兜底常量」也算进 TUNABLE，于是接线做完后可行动点位不降反升（338 → 370）。原因是接线按约定**保留原常量名**（避免把一次参数化放大成一片 diff），接完之后名字里依然带 `Limit/TTL/Threshold`，按名字形状分类时分不出接没接。修法是复用 G4 的**实参位置**判据（常量是否出现在 `Get*(...)` 的兜底实参位，或直连范式里 `Get*` 兜底传 0 而随后回落到该常量），把 `WIRED` 与 `PARAM_FALLBACK`（`Default*` 前缀＝参数化标记，与 G4 的 `DEFAULT_PREFIX` 豁免同口径）单独摘出。**两个脚本口径必须一致**，否则台账说「已参数化」而门说「这是硬编码」，两边数字无法互相校验。

**这条口径的读数会随判据修复而移动，2026-10-12 复扫**：Go 侧 `WIRED 89 / PARAM_FALLBACK 31 / TUNABLE 258`，前端 `CONFIG` 常量 67 行（`TUNABLE 50 / WIRED 17`）。与 2026-10-11 记录的 `14 / 78 / 278` 差得很远，**不是有人新接了 74 条**，而是实参位置判据本身修好了：原先只认裸标识符兜底位，`pkg.Const`、`int(pkg.X)`、`pkg.X*time.Second` 三种形态全部漏判，于是一批真接了的常量被留在 `PARAM_FALLBACK`（按前缀给分）甚至 `TUNABLE` 里。同一趟 `hardcode_inventory.py` 自己扫描 `user-server` 得到的「出现在兜底实参位的常量名」是 **104 个**，比 CSV 的 `WIRED 89` 多 15（按**名字集合**算是 **16 个名字**没有 `WIRED` 行；89 行只有 88 个名字，`DefaultHTTPTimeout`
在两个文件里各坐一个兜底位）。这 16 个的来路本轮逐个查过，**三种各是一种机制、没有一种是漏接**：
- **11 个是小写名**（`wsWriteTimeout`、`wsRegisterTimeout`、`geoPipelineDefaultSEOBatch`、
  `faqAnswerCacheDefaultTTL`，另有 `fallback` / `true` / `false` 三个被兜底位正则误当标识符的局部名）——
  清单的声明面只认 `^[A-Z]`，它们本来就落在 §6 那条「小写名看不见」的盲区里
  （G4 口径现测 277 个，测于 §7.1 注里那棵"能提交的树"），不是清单漏记。
- **3 个坐在兜底位、却被归类器丢进不导出的桶**（`HTTPIngestMaxBodySize`、`HTTPIngestMaxMessages`、
  `L1WindowSize`）：按名字形状被判成 `FIELD_LIT`，而清单只导出 `TUNABLE/PROTOCOL/DEPLOY/INFRA` 四桶——
  G4 自己在 `WIRED_FALLBACK` 里点名了这三条（见 §7.1 的 G4 读数），**属"清单少记三行"而非"少接三条"**。
- **2 个压根不是数字常量声明**：`AutoApprove` 是结构体字段（`d.AutoApprove = GetBool(..., d.AutoApprove)`
  这种自兜底范式），`DefaultLeadIndustryProfile` 是字符串常量（`classify_const` 只认数字/时长/布尔）。

两个数**口径不同、不该互相对账**，写在这里就是为了不让下一个人把差值当成漏项。

**G6 的三条棘轮与实测数字**。G6 不比"绝对覆盖率"，只比趋势——存量太高，卡全量会让 CI
永远红，实际结果一定是大家学会绕过门禁：

- 现测（2026-10-12 复跑 `python3 scripts/check-i18n-coverage.py`，**2026-10-13 在 `f7381630`
  与 `15ccabb7` 两棵影子树上各跑一遍，三趟读数逐字未变**）：**真 i18n 键 183 个 /
  全部 `t()` 去重键 923 个 = 19.83%**，不许跌。
  它不随 tip 摆动不是巧合而是有来历：`f7381630 → 15ccabb7` 那三笔只动了 `.gitignore`、
  `docs/superpowers/specs/ledger/logs/MenuRouteAudit/` 的证据目录、以及 `user-server`
  的 controller／middleware／model／repository／router／service（`git diff --stat` 19 个文件），
  **没有一处 `user-web/**`、没有一处 `locales/**`**——G6 的两个枚举源都不在改动面里。
- **740 处把中文字面量直接当 `t()` 的键传入**，不许增（现测仍是 740，与阶段六持平）。
  这类调用查找必然 miss，
  vue-i18n 回落成 key 本身，于是"中文看起来正常、8 种语言全是中文"。它比不写 i18n 更坏：
  不写至少能被 `hardcode_sweep.py` 数出来，写成 `t()` 之后人 review 时会以为这页已国际化。
- 现测：**词条库无人引用 1455 条 / `zh.json` 1846 条**（78.8%），不许增。阶段六记录的是
  1662/1840（90%）。**这 207 条的降幅不是治理进展，是口径修正**：`LOCALE_ORPHAN` 原先只认
  静态 `t('字面量')`，抓不到 `t('menu.' + key)` 这种拼接形态，于是 `menu.*` 里每一条**合法在用**
  的词条都被算成孤儿；改为按**键**豁免（只豁免后端 `SystemMenuTree` 点名的 208 个 key 折算出的
  `menu.<key>`，注册表之外的死词条照旧计入）之后读数才掉下来。豁免集解析不出来时本门 `rc=2`，
  不留「豁免集为空 ⇒ 全量放行」的假绿位置。词条库是批量生成后与真实页面脱节了，这条账还没清完。
- `resolved_ratio` 19.39% → 19.83% 同样**不是本轮把中文抽走了**：分子那 5 个键来自更早两笔已入库的
  提交（`64b46dd1` / `6d407480`），棘轮基线随之 `--update`。引用这两个百分比时要连这句话一起引用。
- 硬门：9 种语言词条数齐整（现测叶子键数 ar 1856 / ja 1856 / en 1847 / zh 1846 / 其余五种各 1848）。
  「齐整」判的是门自己的那条规则（各语言与 zh 的差值在允许带宽内），**不是逐字节相等**，
  别把上面这串数读成"必须一样多"。

**两处"测量本身是坏的"的坑**（都写进脚本文件头了。这类坑比判据写错更危险：
数字看着仍然是个正数，只是全错）：

1. **只认裸 `t()` 会漏掉 78% 的 i18n 使用**。实测 918 个去重键里 **716 个走 `$t()`**
   （vue-i18n 组合式 API 的全局属性形态），只有 203 个走裸 `t()`。
   —— 这组数是发现当时（阶段五）的读数，今天的分母是 923；比例口径没变，别拿这两个分子对账。
2. **按名字判断"常量是否已接参数中心"必然漂移**。接线时刻意保留原常量名，
   `HTTPIngestMaxMessages` 的 key 是 `bridge.ingest_max_messages`、
   `L1WindowSize` 的是 `memory.l1_window_size`。正确判据是**实参位置**——
   该常量是否出现在 `Get*(ctx, group, key, <常量>)` 的兜底位。

**G3 在 2026-10-12 被重写过一次，因为它当时只在数 9 条**。原实现按"常量名里有没有 `Default`"
去找兜底点，217 条种子里只定位到 9 条可比对，其余全部静默走"解不出 ⇒ 放行"——
一道绿着但什么都没核对的门。重写后的判据与自检（都在 `scripts/check-config-param-defaults.py` 文件头）：

- **认的是实参位置**：`Get*(ctx, group, key, <兜底>)` 第四位。接线按约定保留原常量名，只有位置不会漂移。
- **五种兜底形态**都要认，缺一形态就等于放过那一类参数：
  `A` 直接实参（字面量 / `pkg.Const` / `int(pkg.X)` / `pkg.X*time.Second`）、
  `A0` 兜底位写 `0`＝"不覆盖、沿用下游默认"（如 `misc.upload_max_size_mb`）、
  `B1` 哨兵 0 ＋ 同函数里 `return UPPER`、`B2` `xxxRaw(ctx)` ＋ 调用方 `orX(xxxRaw(ctx), CONST)`、
  `B3` `d := pkg.Default*Config()` 后 `d.Field = orX(Get*(...), d.Field)`（值去构造器字段表里查）。
- **同名多值一律拒绝比对**（`NO_CONST`，标"歧义，不猜"），而不是挑一个值冒充比过。
- **每条种子必须落进四向之一**：`可比对 + 无法比对 + 前端下发 + 已下架 == 种子条目数`，
  不等直接 `rc=2`。前端下发那两组从 G1 用的同一份 `runtimePublicGroups/Keys` 注册表派生，不另抄一份。
- **门自己的健康下界**（这一层是本轮反向测跑出来的，不是防御性代码）：
  常量池为空 ⇒ `rc=2`；`无法比对 > 可比对` ⇒ `rc=2`；
  再加一条**绝对下界棘轮** `scripts/config-param-defaults.baseline`（现登记 184）——
  可比对数掉下来就必须有人来这里解释一次。
  为什么"多数"下界不够：实测把常量声明正则还原成"行尾注释是必要条件"那版（本轮真踩的坑），
  读数是 `可比对 95 / 无法比对 91`，91 不比 95 多，多数下界不火，**只有棘轮拦得住**。
  棘轮由 `--refresh-baseline` 生成，并被 `check-ci-gate-paths.py` 自动派生要求进 CI 触发面。
- 解析面这轮补的三个洞：反引号包裹的 `DefaultValue`（3 条 JSON 种子原先被读成空串）、
  `d.Field := pkg.Default*Config()` 的 `:=` 形式（原先整个 B3 层是死的）、
  负数字面量（`-0.5` 这类阈值原先解不出）。

**门禁抓出并已修的真问题**：

| 门 | 抓出 | 性质 |
| --- | --- | --- |
| G1 | `agent_llm.geo_pipeline_batch_per_run`、`bridge.sse_max_stream_duration` 两条孤儿 key | 代码注释写着"可经该 key 调整"，种子里却没登记 ⇒ 每次读都 miss、每次回落兜底 |
| G3 | `bridge.sse_max_stream_duration` 的种子默认值写成 `"5m"`，而 `duration` 的读取口径是**裸数字按秒**（`GetDuration` 走 `parseDurationSeconds`）⇒ 种子越界、且与代码兜底 `5 * time.Minute` 折出的 300 不是同一个数 | 本门原先没核对种子自己的 Min/Max，这条是补上越界校验后现测出来的；已把种子改成 `DefaultValue: "300"`、`Min 60` / `Max 86400`，Description 写明"0 或负值回落兜底" |
| G3 | `lead.industry_profile` 的兜底位是空串，而种子默认值是一段真文案 ⇒ 没有参数中心的实例里，LLM 提示词的【行业背景】是空的 | 补 `DefaultLeadIndustryProfile` 常量（与种子逐字一致）作兜底，本门从"无法比对"改判"可比对"（184 那条的来路之一） |
| — | `lead.llm_refine_enabled` / `industry_profile` 的注入被塞在 `unified_miner_lead_threshold` 的**读取闭包体内** | 每读一次阈值才顺手装一次 provider：不走统一挖掘路径的读取方永远拿到包级默认（开关默认还是 `false`，与种子 `true` 相反）。已提出来独立装，并把包级默认改成与种子一致 |
| G4 | `memory.l1_window_size` 真漏接，且 `BuildFullContext` 里还有一处直接传常量 | LLM 提示词的生产构造入口绕过了参数；同文件 `L1List`/`l1Trim` 都读参数、唯独提示词不读 |
| — | `audit_api_contract.py` 按名推断表缺 `plainAuth`/`admin` ⇒ 3.2/4.1 新增的两个端点被判"后端没有" | 已按该脚本既有规则补齐，UNMATCHED 2→0 |
| — | `check-hardcode-tunable.py` 跳过目录判定用绝对路径段 ⇒ 仓库检出在 `/tmp` 下时每个文件都被跳过 | 主仓路径不含 `tmp` 段，门一直是绿的；**干净 worktree 验证抓出来的** |
| G4（门自己） | 反向测豁免前缀时漏删它前面那根竖线，交替表出现空支（`^(A|B|)`）⇒ `EXEMPT_NAME.match(任何名字)` 恒真，读数当场从「兜底 **3** / 豁免 **52**」跳成「兜底 **0** / 豁免 **77**」而 **rc=0** | 少掉的 25 个不是消失了，是被空支吞进豁免桶——**门变成橡皮章且自己不出声**。现在 `main()` 第一件事是 `check_alternation_shape()`（`scripts/check-hardcode-tunable.py:278`），对 `TUNABLE_SUFFIX`/`EXEMPT_NAME`/`DEFAULT_PREFIX`/`SEMANTIC_BOUND` 四张表查 `||`、`(|`、`|)`，命中即 fail。**空交替的两种坏法方向相反**：判红偏绿的豁免表要自检自己发，判绿的词缀表（`TUNABLE_SUFFIX`）空支会把所有常量判成旋钮，同样当场停。<br>**本轮把这两句都跑成了读数**（改前 `cp` 备份、改后 md5 对回 `068fa034…`）：把 `EXEMPT_NAME` 尾部改成 `ClueType|)` ⇒ 门退 2 并打印出整条 pattern；同一处空支再摘掉自检调用重跑 ⇒ 正是「兜底=0 / 豁免=77 / rc=0」。同轮还量到**这条自检的边界**：只删前缀、不产生空支（删 `BGEM3|`，下一行以 `ClueType` 续上）时门**不会**报警，读数只从豁免 52 变 51 ⇒ 自检守的是"判据恒真"这一类，**豁免表被改短不在它的视野里**，那要靠自己读那行计数 |
| 装配层对账（非六门） | `agent_tool.fail_threshold` / `agent_tool.cooldown_duration` 的**假接线**：provider 在 `agent_tool_params_wiring.go:45,50` 注进去了，可 `InitGlobalToolRouter` 往 `NewToolRouter` 传的是字面量 `5` / `30 * time.Second`，而 `NewToolRouter` 只在**非正值**时才回落 accessor（`tooluse/tool_router.go:58,61`）⇒ 正数字面量把那条回落路堵死，参数中心改了永远不生效 | 本轮把 `internal/app/*_wiring.go`（42 个文件、**80 处** `Set*Provider(` 注入点，setter 名各不相同）**逐条核到消费方**：光看"注没注"不够，必须看消费点取的是 accessor 还是字面量。这一趟只有 1 处不合格，就是上面那条（已改走 `tooluse.RouterFailThreshold()`/`RouterCooldownDuration()`）。锁它的用例是 `internal/app/agent_tool_params_wiring_test.go` 的 `TestToolRouterConstructionReadsAccessors`——`RouterConfig` 的两个字段是私有字段、`GetStats()` 不回传，行为层读不出"构造时取了谁"，所以是静态锁；三条变异现测：**accessor 换成字面量 ⇒ A 腿（恰好 1 次）红**、**字段接字面量而 accessor 齐全 ⇒ B 腿红并打印真实行号（第 123 行）**、**新增一条写着限定名 `tooluse.RouterFailThreshold()` 的注释 ⇒ 仍绿**（证明按行判据跳注释这件事在守，不是装饰） |

| group | 僵尸 key |
| --- | --- |
| `misc`(16) ✅已接线 | `visitor_token_ttl`、`sso_cookie_ttl`、`polling_lock_stale_threshold`、`domain_check_concurrency`、`sms_max_retry`、`csv_export_max_rows`、`backup_page_size`、`text_truncate_max_bytes`、`preview_max_len`、`sop_scheduler_interval`、`sop_max_wait`、`deepl_timeout`、`reply_sem_timeout`、`ownership_cache_ttl`、`summary_stale_threshold`、`agentloop_history_max_candidates` |
| `confidence`(5) ✅全部接线 | `humanize_default_threshold`、`humanize_boundary_low`、`humanize_boundary_high`、`humanize_sample_rate`、`humanize_max_retry`（5 条均在 3.1a，`service/humanize/service.go`）、`intent_fewshot_min_cos`、`weak_truth_min_confidence`、`emb_retry_cooldown`、`veto_low_rag_threshold`。原 7 条里的 `persona_default_threshold`/`persona_max_retry` **已于 2026-10-11 下架**：其实现 `persona_evaluator.go` 已随 `9a5f716c` 整体删除（749 行实现 + 958 行测试），这两条永远不会有读取点，等价能力是 humanize（其五条已在 3.1a 接线），留在页面上只会诱导后来人去查一个已经删掉的实现 |
| `agent_llm`(7) ✅已接线 | `vote_agreement_threshold`、`default_health_check_interval`、`default_circuit_open_duration`、`default_health_check_timeout`、`default_failure_threshold`、`default_http_timeout`、`db_sink_stop_deadline` |
| `cache`(5) ✅已接线 | `max_keys`、`faq_decay_max_batch`、`memo_ttl`、`platform_cache_ttl`、`translation_cache_max_entries` |
| `agent_tool`(5) ⚠️4条已接线 | `max_concurrent`✅、`max_content_len`✅、`cooldown_duration`✅、`fail_threshold`✅；`result_cache_ttl` **无处可接**——`tooluse/result_cache.go` 的 `NewResultCache` 全仓只有 `p2_test.go` 调用，生产链路从未构造过它，没有构造点就接不上 TTL，保留「未接线」标注 |
| `telemetry`(4) ✅已接线 | `node_health_window`、`trace_sink_buffer`、`geo_position_window`、`feature_flag_poll_interval` |
| `workflow`(4) ✅已接线 | `max_subflow_depth`、`max_workflow_steps`、`max_running_per_sop`、`max_executed_node_trace` |
| `inbox_sales`(4) ✅已接线 | `tg_lead_opportunity_threshold`✅（`service/telegram_lead_miner.go`）、`unified_miner_lead_threshold`✅（`service/lead_miner_unified.go`）、`preview_sample_limit`✅（`service/oneid_merge_preview.go`）、`geo_lead_preview_max_len`✅（`service/trace_learning/insights.go`，seam 本就存在，只补注入） |
| `bridge`(3) ❌无处可接 | `polling_max_timeout`、`polling_default_timeout`、`max_reply_content_bytes` **无处可接**——`handler_http.go` 的两个长轮询常量全树只有 `defaults_test.go` 引用（文件注释自陈「服务端没有长轮询实现」），`bridge_helpers.go` 的 `maxReplyContentBytes` 自带 `//nolint:unused`，生产路径无任何回复截断 |
| `session`(3) ✅已接线 | `active_ttl`✅（`repository/customer_session.go`，service 侧改为直接读同一读取口以保持单一源）、`idle_ttl`✅（`aiagent/mcp/http.go`）、`max_delay_seconds`✅（`content/service/marketing_flow.go handleDelay`；**种子原描述「触达排程超期放弃」与实现不符——实现是「delay 节点等待上限、超过截断」，已一并改写对齐**） |
| `pagination`(3) ⚠️1条已接线 | `cursor_page_size`✅（`pkg/pagination/cursor.go`，经 `utils.ParseCursorParams`→`ClampLimit` 被 operation_log/security_audit/customer 三处生产调用）；`page_max_size`/`page_default_size` **无处可接**——对应的 `ParsePagination`/`ParsePaginationOffset` 全树只有 `pagination_test.go` 调用；`page_default_size` 在 `ParseCursorParams` 的兜底分支又被三个调用方的正数 `defaultLimit` 遮蔽。另注种子写 100、代码 `defaultMaxPageSize` 写 200 本就不一致 |
| `wechat`(3) ❌无处可接 | `chat_ws_ping_period`、`chat_ws_pong_wait`、`chat_ws_write_wait` **无处可接**——常量在 `controller/chat_ws.go` 有读取点，但承载它的 `router/ws.go RegisterWSRoutes` 全树零调用方（`GET /ws/chat` 从未注册，实测落到 SPA NoRoute 兜底）。另：后端无微信渠道 WS 客户端，`internal/channelbot` 只有 core/qq/telegram/whatsapp |
| `sales`(2) ⚠️1条已接线 | `identity_max_attempts`✅（`service/customer_identity.go findExistingWithRetry`）；`insight_limit` **无处可接**——`trace_learning.TopInsights` 的 `limit` 是**入参不是默认值**，全仓唯一调用方是 `insights_test.go`（且三处都硬传 3），生产链路没有任何地方取这个数量 |
| `middleware`(2) ✅已接线 | `audit_flush_interval`✅（`middleware/audit.go processAuditLogs` 的 `time.NewTicker`）、`mfa_recent_verify_ttl`✅（`middleware/mfa.go`，4 个读取点：Redis Set / 近期校验 / 内存表校验 / 后台清理） |
| `knowledge`(2) ✅已接线 | `max_upload_file_size`✅（`knowledge/service/knowledge_base_import.go`，5 个读取点 + `knowledge_service_import.go` 4 个；导出 const 已改为函数）、`merchant_knowledge_max_len`✅（`knowledge/service/knowledge_merchant_external.go splitMarkdownToItems` 的 flush 闭包内，原本是闭包内的局部 const） |
| `wecom`(6) ✅全部接线 | 2d 接了 `quota_degrade`（并顺带修掉「配额降级半接线」：`-25` 档原写死 0.95，改参数调到 0.95 以上时高档先命中、参数根本轮不到生效）；**2026-10-11 终验时拆成六条**：配额 `quota_min_rate`/`quota_degrade`/`quota_critical_rate` + 成功率 `success_rate_critical`/`success_rate_degrade`/`success_rate_warn`。原 `error_rate_degrade` 随之下架——它标未接线的理由「单一阈值表达不了递减分档」成立，但结论不成立：**正解是拆成多条独立参数而不是放弃**。附带查出该参数名不副实：它叫「错误率」而 `computeHealthScore` 判定用的是「成功率」，互补数拿一个当另一个用会得出方向相反的结论。递减关系由 `runtimeWeComQuotaTiers`/`runtimeWeComSuccessRateTiers` 用 clamp 强制，运维乱序填值也不会把扣分方向搞反 |
| `channelgw`(1) ✅已接线 | `ws_push_interval`✅（`channelgw/ws.go runtimeWSPushIntervalDefault`）——**但这条原本是门禁抓不到的假接线**：读取点存在，key 却是 `"ws_push_interval_default"`，种子里登记的是 `"ws_push_interval"`，多一个后缀导致每次读都 miss、每次都回落兜底 2s，参数改了永远不生效。同组另外 4 条 key 都对，是单点笔误。readpoints 门检查的是「种子 key 有没有读取点」，看不出读取点用的是别的 key，本批已改正并补真库行为测试钉住 |

### 7.2 余下 13 条「已声明未接线」：为什么它们接不了（2026-10-11 终验逐条核实，2026-10-12 补两条）

> **本节已于 2026-10-11 阶段六兑现**：当时余下的 11 条全部改为**下架**（`deprecated=true`），
> 从「过渡态」推到「终态」，`已声明未接线` 归零。下面这张表保留原样作为
> 「为什么当初接不上」的依据——将来谁要复活其中任何一条，先读这里的原委，
> 别以为那就是「加个读取点」的活。三段只被测试引用的死代码常量同时删掉。
> 详见 `CONFIG_DB_DRIVEN_PLAN.md` §6.1~6.2。
>
> **2026-10-12 又进来两条**（表末两行，`frontend_ui` 组）：这两条不是新发现的病，
> 是 G1 前端判据换形状之后**才量出来的**——原实现按组白名单认定「前端在读」，
> 它们一直躲在"已接线"里（§7.1 总读数那段的 `206 → 204` 就是这两条）。


方案 §8 验收标准 5 原写「参数中心页面上不存在任何标着（未接线）的条目」。这一条**方向错了**：标着「为什么接不上」比偷偷留一个永远不生效的旋钮要好。真正必须为 0 的是 readpoints 门里的 `UNDECLARED`（有旋钮没人读也没人承认）和 `STALE`（已接线却还标着未接线），这两项现在都是 0。

余下这 13 条按「接不了的原因」分四类。**接不了 ≠ 没做工作**：前两类是死代码事实，强行接线只会造出「读取点存在但整条链路是死的」假接线——阶段 1.8 在 `channelgw.ws_push_interval` 上就栽过这种（key 写错导致每次读都 miss，而门禁判它已接线）。

| key | 接不了的原因 | 该做什么（不是接线） |
| --- | --- | --- |
| `bridge.polling_max_timeout`<br>`bridge.polling_default_timeout` | 服务端没有 HTTP 长轮询实现：`ingest` 立即返回，回复走 SSE 或 `/outbox` 轮询。两个常量全树只有 `defaults_test.go` 引用 | 要长轮询时先把实现建起来，再把常量当 fallback 传给 `GetDuration` 并给等待加 `ctx.Done()` 退出 |
| `bridge.max_reply_content_bytes` | `bridge_helpers.go` 的 `maxReplyContentBytes` 自带 `//nolint:unused`，生产路径没有任何回复内容截断逻辑 | 要在出网前对该字段做截断，再接本参数 |
| `pagination.page_max_size`<br>`pagination.page_default_size` | `ParsePagination`/`ParsePaginationOffset` 全树只有 `pagination_test.go` 调用；`page_default_size` 的另一处消费（`ParseCursorParams` 的兜底分支）又被 operation_log/security_audit/customer 三个调用方的正数 `defaultLimit` 完全遮蔽。另：种子写 100、代码 `defaultMaxPageSize` 写 200 本就不一致 | 先决定列表分页到底走 `ParsePagination` 还是 `ParseCursorParams`，把另一条路径下掉或补上生产调用方 |
| `wechat.chat_ws_ping_period`<br>`chat_ws_pong_wait`<br>`chat_ws_write_wait` | 常量在 `controller/chat_ws.go` 有读取点，但承载它的 `router/ws.go RegisterWSRoutes` **全树零调用方**——`GET /ws/chat` 从未注册，实际落到 SPA 的 NoRoute 兜底。另：后端根本没有微信渠道 WS 客户端（`channelbot` 只有 core/qq/telegram/whatsapp），种子的「微信渠道 WS」名不副实 | 修 `/ws/chat` 路由（后端确实需要它）或删掉整段死代码。这属于功能债不是配置债 |
| `agent_tool.result_cache_ttl` | `NewResultCache` 全仓只有 `p2_test.go` 调用，生产链路从未构造过 `ResultCache`——没有构造点就接不上 TTL | 要启用先在 executor 装饰链上挂 `ResultCache` |
| `sales.insight_limit` | `trace_learning.TopInsights` 的 `limit` 是**入参不是默认值**，唯一调用方是测试（且三处都硬传 3） | 要有一个生产调用方把数量传进来，才有默认值可言 |
| `lock.ai_lock_ttl` | `tryAcquireAILock`/`ReleaseAILock`/`IsSessionAIBusy` 三个函数整条生产链路都没人调用（源文件自带 `//nolint:unused`）；真正在跑的并发闸是 `lock.ai_processing_ttl`（2c 已接） | 与 wecom 那三条同理：要么删死代码，要么在生产链路上真正启用这把锁 |
| `frontend_ui.websocket_reconnect_max_wait_ms`（2026-10-12 新增） | 与 `frontend_ws.max_reconnect_delay_ms` **同义重复**：坐席/聊天两个 WS 客户端的退避上限都由后者提供（`utils/agentSocket.js:224`、`utils/chatSocket.js:310` 各一处 `rtDur`），而本键在 `user-web/src` 全树**零引用**（实测 `grep -rn` 命中 0） | 要复活就先让某一类 socket 真读它，再把两条合并成一条；**不要回到"两条都能改、只有一条生效"的形态** |
| `frontend_ui.toast_duration_ms`（2026-10-12 新增） | 前端**没有统一的提示出口**：全树 196 个文件各自调 `ElMessage`，只有 3 处显式传 `duration`，而 Element Plus 没有「消息默认时长」的全局配置项可挂。本键同样零引用 | 要接上得先在 `utils/` 收敛一个提示封装（覆盖 `ElMessage.success/error/warning` 三形）并把那 196 处改走它——这是一次前端改造，不在参数中心能单独完成的范围 |

**若要清零这 13 条**：8 条要动的是死代码与路由（功能债），3 条要动的是产品决策（分页走哪条路径、洞察给多少条、结果缓存要不要启用），另外 2 条（`frontend_ui` 那两条）要动的是**前端有没有统一出口**——一条是结构改造、一条是键合并。没有一条是「再花半小时接线」能解决的，所以把它们挂在门禁上等 CI 变红（`UNDECLARED`/`STALE` 为 0）比对它们永远显示「未接线」更诚实。

**逐条对上了本清单 A 桶的硬编码**（这就是"接线"的具体施工单）：

| 僵尸参数 | 对应硬编码点位 |
| --- | --- |
| `misc.visitor_token_ttl` | `pkg/security/visitor_token.go:14 DefaultVisitorTokenTTL=7` |
| `misc.sso_cookie_ttl` | `controller/sso.go:20 SSOCookieTTL=5` |
| `misc.polling_lock_stale_threshold` | `repository/telegram_polling_lock.go:20 =60` |
| `misc.sms_max_retry` | `model/sms_tracking.go:70 MaxRetry=3` |
| `misc.csv_export_max_rows` | `ops/service/custom_report_export.go:16 =30000` |
| `misc.text_truncate_max_bytes` | `pkg/textutil/truncate.go:8 DefaultMaxBytes=8192` |
| `agent_tool.fail_threshold` / `cooldown_duration` | `tooluse/tool_router.go:59,62 =5,30` |
| `agent_tool.max_content_len` | `service/message_hub.go:175 MessageHubDefaultMaxContent=64`（同类项） |
| `agent_llm.default_*`（4 条熔断/健康） | `llm/provider_failover.go:32-35` |
| `confidence.humanize_default_threshold` / `persona_default_threshold` / `persona_max_retry` | `service/humanize/service.go:16,28`、`service/persona_evaluator.go:585,587` |
| `cache.translation_cache_max_entries` | `rag/retrieval/translation_cache.go:22 =100000` |
| `bridge.polling_max_timeout` / `polling_default_timeout` | `bridge/handler_http.go:37,38 =500,30` |
| `session.active_ttl` | `repository/customer_session.go:24 =24h` ✅（service 侧 `CustomerSessionActiveTTL` const 保留为兜底值声明，运行时改读 `repository.SessionActiveTTL()`） |
| `session.idle_ttl` | `aiagent/mcp/http.go:17 sessionIdleTTL=30m` ✅ |
| `session.max_delay_seconds` | `content/service/marketing_flow.go:527 函数内 const =300` ✅（**种子原描述与实现不符，已改写**） |
| `pagination.cursor_page_size` | `pkg/pagination/cursor.go:14 CursorPageSize=100` ✅ |
| `pagination.page_max_size` / `page_default_size` | `pkg/utils/pagination.go` 的 `ParsePagination`/`ParsePaginationOffset` —— **零生产调用方**（详见上表行内说明） |
| `inbox_sales.tg_lead_opportunity_threshold` | `service/telegram_lead_miner.go:25 =40` ✅ |
| `inbox_sales.unified_miner_lead_threshold` | `service/lead_miner_unified.go:48 =40` ✅ |
| `inbox_sales.preview_sample_limit` | `service/oneid_merge_preview.go:40 =20` ✅ |
| `inbox_sales.geo_lead_preview_max_len` | `service/trace_learning/insights.go:25 =200` ✅（seam 本就存在，只补注入 + 非正值守卫） |
| `wechat.chat_ws_ping_period` 等 3 条 | `controller/chat_ws.go:51-55` —— 常量有读取点但 `/ws/chat` 路由从未注册（详见上表行内说明）；另前端 `utils/chatSocket.js` 有对应常量属**前端**阈值，归阶段三 |
| `wecom.error_rate_degrade` | `wecom_account_health.go:45 =0.3` ❌（**无读取点**，且常量旁的注释谎称「运行时通过 GlobalConfigParam() 读取」，已一并修正；实现是写死三档，单一阈值接不上） |
| `telemetry.feature_flag_poll_interval` | `pkg/featureflag/flag.go:34 =5` ✅ |
| `workflow.max_subflow_depth` | `workflow_node_executors.go:14 =5` ✅ |
| `telemetry.node_health_window` | `internal/monitor/monitor.go:19 nodeHealthWindow=24h` ✅ |
| `telemetry.trace_sink_buffer` | `internal/aiagent/llm/trace_sink.go:35 dbSinkBufferSize=2048` ✅（**种子原写 8192，已按代码回填为 2048**） |
| `telemetry.geo_position_window` | `internal/geo/service/metrics.go:71 positionWindow=20` ✅（**单位是字符偏移不是条数，种子 Name/Description 已一并改写**） |
| `workflow.max_workflow_steps` | `internal/service/workflow_dispatcher.go:153 函数内 const =1000` ✅ |
| `workflow.max_running_per_sop` | `internal/service/sop_scheduler.go:233 函数内 const =50` ✅ |
| `workflow.max_executed_node_trace` | `internal/service/sop_dispatcher.go:706 =200` ✅ |
| `sales.identity_max_attempts` | `service/customer_identity.go:335 函数内 const maxAttempts =8` ✅ |
| `middleware.audit_flush_interval` | `middleware/audit.go:58 函数内 const flushInterval =5s` ✅ |
| `middleware.mfa_recent_verify_ttl` | `middleware/mfa.go:21 mfaRecentVerifyTTL =5m` ✅（4 个读取点） |
| `knowledge.max_upload_file_size` | `knowledge/service/knowledge_base_import.go:35 MaxUploadFileSize =50<<20` ✅（导出 const 已改为函数；另 `knowledge_service_import.go` 4 处） |
| `knowledge.merchant_knowledge_max_len` | `knowledge/service/knowledge_merchant_external.go:483 flush 闭包内 const maxLen =2000` ✅ |
| `channelgw.ws_push_interval` | `channelgw/ws.go:41 =2s` ✅（**原为假接线**：读取点 key 写成 `ws_push_interval_default`，种子里是 `ws_push_interval`，已改正；真库行为测试已钉住） |

→ **接线动作就是把上表右列的常量替换为 `config.GetInt/GetDuration(...)`，然后把种子里的
「（未接线）」标注撤掉**（门禁第 2 条会检查标注是否过期）。
这一步零新增表、零新增 API、零前端改动，是投入产出比最高的一批。

---

## 8. 已知遗留问题

> **本节四条已于 2026-10-12 逐条实测复核：全部已闭环。** 留原说法与实测证据，
> 是因为这四条曾经被当成"实施阶段的前置修复"挂在脸上，谁也不能凭印象说它修没修。
> 每条都写「原说法 → 实测（怎么验的）→ 处置」。

1. **原说法**：`config_param_test.go` 的 `const defaultParamDefsWant = 114` 与读取点门实测的
   115 漂移 1 条。
   **实测**：该锚点现为 `defaultParamDefsWant = 217`（`config_param_test.go:274`），
   `TestDefaultParamDefsCount` 与门读数的种子条目数一致（G1/G3/G5 三处都打印 217）。
   **处置**：阶段 0 当时补到 115，之后随批次一路上到 217；这条计数本身由用例与三道门同时盯着。
2. **原说法**：`config_params` 的实际索引是 `UNIQUE(key)` 单列，而 gorm tag 写的是复合
   `uniqueIndex:idx_group_key` ⇒ key 必须全局唯一，跨 group 同名 key 会在 seed 时撞索引并被
   `logger.Warnf` 静默丢弃。
   **实测**：`internal/migration/migrations/v3_47_0_config_param_group_key_unique_migration.go:60`
   把 `idx_group_key` 重建为 `CREATE UNIQUE INDEX ... ON config_params (param_group, key)`，
   并带 `DROP`/回滚两条路径与同名迁移用例（`..._test.go:89` 断言索引定义是复合、
   `:92` 断言它仍是唯一索引——复合不等于普通索引）；模型标签
   （`model/config_param.go:17-18`，Group priority:1 / Key priority:2）与之相符；
   `SeedConfigParams` 的插入失败也不再静默，汇总成 error 返回。
   **处置**：跨 group 同名 key 现在是**合法**的两条参数行；因此 §2.2 那条"key 必须全局唯一"
   的推论作废，唯一性口径改成 G5 的**组内不重名**（读取侧 `GetByGroupKey`/`UpdateValue`
   都按 `(group,key)` 定位，组内重名才会让更新打到错误行）。
3. **原说法**：`Service.validateValue` 的 `duration` 分支在 `time.ParseDuration` 失败时
   直接 `return nil`，跳过 min/max 校验 ⇒ 可被利用的绕过口。
   **实测**：现读 `config_param.go` 的 `validateValue`，`duration` 分支走
   `parseDurationSeconds(value)`，`err != nil` 时 `return err`；min/max 同样经
   `parseDurationSeconds` 折算成秒再比大小，边界值自身不可解析时报错而不是放行。
   **处置**：已闭环，且与 `GetDuration` 共用同一个解析函数（校验口径＝读取口径，
   否则会出现"校验过了但读出来是另一个数"）。
4. **原说法**：`NewSystemConfigKVRepository()` 捕获调用那一刻的 `db.GetDB()`，为 nil 时
   `Get` 会 panic 而非返回 error。
   **实测**：`system_config_kv.go:31` 有 `ErrKVStoreUnavailable`，`Get`/`Upsert`（以及
   `EnsureTable` 一路）先过 `Available()`（`:56-58`，判 `r != nil && r.db != nil`），
   拿不到句柄时返回该 sentinel 而不是崩；装配点要句柄确定性用
   `NewSystemConfigKVRepositoryWithDB`（`:51`）。
   **处置**：已闭环。注释里写清了为什么不把它当成"没配过"——"没配过"是合法业务状态，
   两者在 `Get` 里都表现为空值，合成一种返回会让"运维改了配置不生效"无从查起。

原来的收尾句「以上 4 项属**实施阶段的前置修复**，详见 `docs/CONFIG_DB_DRIVEN_PLAN.md` §4 阶段 0」
已经兑现，不再作为待办挂在这里。

### 8.1 「七道门都挂在 CI 上」这句话该怎么验（2026-10-13 现取）

这句话本轮要对账，而**按脚本名去 grep 步骤名会得到 0 命中**——`user-server-ci.yml` 里七个步骤的
`- name:` 全是中文标题（`参数中心读取点门（要么有人读、要么承认未接线）`、`文本编码门（非 UTF-8 字节 …）`），
脚本路径只出现在 `run:` 行。现测（`grep -c` 逐条列在下面，全部在本文件内跑）：

| 口径 | 结果 | 为什么它不是答案 |
| --- | --- | --- |
| `- name:` 行里含脚本名 | 六枚门各 **0** | 步骤名是给人读的中文；按它数门＝"CI 里一道门都没挂" |
| `run: python3 scripts/<门>` 行 | 六枚门各 **1**（G1 `:228`、G3 `:246`、G4 `:251`、G5 `:256`、G6 `:262`、G7 `:279`） | 这才是"执行点"的唯一合法口径；两枚电池另有 `run: bash scripts/<门>.test.sh`（`:240`、`:293`） |
| 按 basename 前缀 `scripts/<门>` 全文件计数 | G1 与 G7 各 **6** | 其中 4 行是 `on.push.paths`／`on.pull_request.paths` 的**注册**行（`:23/:27`、`:78/:80`、`:41/:45`、`:93/:95`），2 行才是 `run:`；拿它当"执行了几个步骤"会把注册面读成执行面，还会把门与它的用例算成同一族的两次 |
| 中文标题正则 `name: (参数中心\|文本编码\|新增可调\|i18n)` | 命中 7 行 = 6 道门 ＋ 1 道用例（`参数中心读取点门用例`） | 标题里"门"与"门用例"共用前缀，**计数比真门数多 1**；而且它匹配不到 `架构合规检查(分层)` 这类不带"门"字的步骤 |

结论：**"某道门有没有 CI 执行点"只能问 `run:` 行**，问法固定成
`grep -n "run: python3 scripts/<脚本名>" .github/workflows/*.yml`；再要判"改了它 CI 会不会重跑"，
那是另一条轴（`on.*paths` 里有没有这一行），两轴必须分开数——`check-ci-gate-paths.py` 守的就是
第二条轴，它 rc=0 只说明"注册齐"，不说明"执行点在"（本轮两枚电池的 `paths` 注册与 `run` 各有 2 行，
正是这两轴各自独立的最直接样本）。顺带一条同样形状的教训：`make audit` 的 31 节里**没有**
`golangci-lint`（本轮 `grep -c golangci` 那份整链日志＝0），所以"跑过 `make audit`"绝不能写成
"跑过 lint"，lint 只在本机与 CI 的 `static-gates` 作业里各有一个执行点。

### 3.1 F 桶落地进展（2026-10-11，阶段 4.1~4.3）

已在 `dictionaries` / `dictionary_transitions` 两表落地 **5 个状态机域**（21 项 + 22 条跃迁），
与 `model/*.go` 常量逐字对应：

| parent_code | 项 | 跃迁 |
| --- | --- | --- |
| `approval_status` | 4（pending/approved/rejected/expired） | 3 |
| `bad_case_status` | 3（pending/labeled/exported） | 2 |
| `human_task_status` | 5（open/assigned/in_progress/completed/cancelled） | 6 |
| `opportunity_stage` | 6（lead/qualified/proposal/negotiation/won/lost） | 7 |
| `payment_status` | 4（created/paid/refunded/closed） | 3 |

**未落地的 39 项及原因**（逐类写清，不留给下一个人猜）：

- **状态机类未落地的 6 项**（`BillStatuses` / `KBChangeStatuses` /
  `OrderDraftTerminalStatuses` / `QuoteStatuses` / `OpportunityStatuses` /
  `OpportunityOutcomes`）：它们的跃迁规则没有显式表达过（代码里只有常量数组），
  落字典表会造出一张空跃迁表——那比没有表更糟（看着像有状态机约束，实际谁都能跳）。
  要落得先补跃迁事实。
- **`SystemRoleList` 不落**：角色是 `roles` 表驱动的主体，字典只是它的展示枚举。
  两份各说各话的风险大于收益，且角色的权限语义在 `permission_matrix`。
- **分类标签类 13 项不落**：它们多数带展示属性（tagType/sort），属于前端
  `src/constants/*.js` 那一侧，与后端字典表的 `code/name/description/sort_order`
  不是同一种东西。硬搬要么给后端加展示专用列，要么前端丢掉 tagType。
- **流程步骤类 4 项不落**：`DefaultPipelineSteps` / `DefaultSendPipelineSteps` 是**默认值**
  不是值域（用户可自定义整条流水线），属 `config_params` 语义而非字典语义；
  `LTCKnownStages` / `LTCKnownThresholdKeys` 是 LTC 配置的键名清单，同前者。
- **意图/提示词类 4 项不落**：`DefaultIntents` 是带训练向量的意图定义，
  `SystemPromptTemplates` / `DefaultLongtailTemplates` 是长文本模板，
  都不是「状态 + 跃迁」，塞字典表要把 `param_value` 变成第二份 JSON 泥潭。
- **`HumanTaskSLAColumns` 不落**：真·DB 列名，属 MODEL 不属配置。

**读取口径**：后端 `DictionaryService.GetDictionary` 是「**DB 优先，DB 没有这一批就回落
Go 种子**」。不反过来（常量优先、DB 只做覆盖）——字典描述的是存量行正在用的值域，
反向会让管理端改了不生效。前端 `useDictionaryStore` 与之后端一致，失败静默回落本地常量，
且**不写进 loaded**（否则后端恢复后前端会永久用本地常量）。

**种子只增不改**：与 `config_params` 的 DefaultValue 不同（那边是「没人配置时的行为」，
可随代码演进调整），字典种子是**存量数据的值域**，删一项等于让历史行变成非法值。
要停用一项走 `Enabled=false`，不删行。`SystemLocked` 项管理端不能改名或停用。

---

## 9. 阶段六收官：配置治理的最终账（2026-10-11）

### 9.1 两类数字（起点 → 终点）

| 指标 | 起点 | 终点（2026-10-12 现测，复现见 §0） |
| --- | --- | --- |
| 参数中心种子条目 | 115 | **217** |
| wired（有读取点且未挂标注） | 40 | **204** |
| UNDECLARED（有旋钮没人读也没人承认） | 72 | **0** |
| STALE（已接线却还挂未接线标注） | — | **0** |
| ORPHAN（代码在读而种子没登记） | 未查 | **0** |
| 已声明未接线 | 72 | **0**（13 条改为下架） |
| 已下架（deprecated，含下架理由） | 0 | **13** |
| Go CONFIG TUNABLE（清单口径） | 338 | **258** 未接（另 `WIRED` 89 / `PARAM_FALLBACK` 31 ＝ 已接 120，可迁总量 378） |
| 前端 CONFIG 点位 | 65 | **67**（`TUNABLE` 50 待接 / `WIRED` 17 已接） |
| 总清单点位 | 7725 | 7745 → **7904** |
| 配置页可见的中文分组名 | 29（硬编码在前端） | **35（读后端 `config_param_groups`）** |
| 字典管理 UI | 无 | **有（`/system/dictionaries`）** |

「已声明未接线 → 0，已下架 → 13」这一行是本阶段的全部意义：
门禁的 `UNDECLARED` / `STALE` / `已声明未接线` 三类现在**同时为 0**，
而那 13 个旋钮的去向是明确的（终态 + 理由 + 复活前提），不是消失了。

### 9.2 本阶段补齐的「友好性」缺口（任务二）

配置能不能被友好地配置，取决于后端有没有数据给 UI 显示。三个缺口各自补齐：

| 缺口 | 补法 | 规模 |
| --- | --- | --- |
| 用户看不懂数字的单位 | `ConfigParam.Unit` | 95 条种子填了单位；`duration`/`bool` 一律不填（单位恒为秒或不存在） |
| 用户不知道填错会不会出事 | `ConfigParam.Risk`（闭集 `""`/`warning`/`danger`） | 52 条：`danger` 31 条填错直接挂，`warning` 21 条静默改语义 |
| 新分组没有中文名（29 组硬编码在前端） | 新表 `config_param_groups` | 35 组，含 label / description / sort_order |

配套的界面改动（`ConfigParams.vue` 整页重做 + `DictionaryManager.vue` 新建）
见 `CONFIG_DB_DRIVEN_PLAN.md` §6.5~6.6。

### 9.3 一条不该被当成「完成」的账：G6 的 i18n 覆盖率（收官时 19.39%，2026-10-12 复扫 19.83%）

阶段五的 G6 门加了三条**棘轮**（覆盖率只许涨、中文直传只许跌、词条孤儿只许不增），
它防的是倒退，不是欠账。截至本阶段收官：

- `t()` 调用里 918 个去重键，**真 i18n 键只有 178 个**（19.39%）；
- **740 处把中文当 `t()` 的键直接传**，查找必然 miss，vue-i18n 静默回落成 key 本身
  ⇒ 中文看起来正常、8 种语言全是中文。这比不写 i18n 更坏：
  不写至少能被扫描器数出来，写错了看起来像做了；
- 1662/1840 条词条无人引用（90%）。

> 上面三个数是 **2026-10-11 的收官快照**。2026-10-12 复扫读数、以及「孤儿词条 1662 → 1455 属
> G6 拼接键口径修正、不是治理进展；覆盖率 19.39% → 19.83% 的分子来自更早两笔已入库提交」
> 这两条归属判定，见 §7.1 的「G6 的三条棘轮与实测数字」。别把降幅当成已经还掉的账。
> **2026-10-13 在 `f7381630` 与 `15ccabb7` 两棵影子树上各复跑一遍，两趟逐字相同**：
> `t() 去重键 923 / 真 i18n 183（解析不到词条库 0）/
> 中文字面量直传 740 / 词条库无人引用 1455（zh 词条 1846 条）/ resolved_ratio 19.83%`，
> 与 10-12 那趟逐字相同——上面 918／178 两个数是 10-11 快照，已过期，别再引它们做进度。

这三项**属于文案治理不是配置治理**，本轮没有动，记在这里作为下一轮的真实入口。
前端 UI 本轮新增的中文文案同样落在 740 这个口径里——这是有意的取舍：
先把配置页的能力做完整（分组中文名、下架原因、风险提示），
再统一做 i18n 抽取，否则要先为一批会变的文案建词条。
G6 棘轮确保它不会再退。

### 9.4 已下架的 13 条（下架 ≠ 消失；2026-10-11 下架 11 条，2026-10-12 补 2 条）

现测 `grep -c "Deprecated: true" internal/service/config_param_seeds.go` = **13**
（HEAD 那份是 11；多出的两条是 §7.2 表末的 `frontend_ui.websocket_reconnect_max_wait_ms`
与 `frontend_ui.toast_duration_ms`，两条都带 `DeprecatedReason`）。
**「一一对应」这句在 10-12 写下时是没被证过的**：种子标了 13 条，而钉清单边界的用例
`config_param_retire_test.go` 的 `retiredSeedKeys` 仍是 11 条，直到 10-13 在影子树全量复跑
`./internal/service` 才红出来（`TestRetiredParamsAreExactlyTheElevenOnes` 报两条"被标了
Deprecated 但不在清单里"、`TestListHidesRetiredParamsByDefault` 报差值 13 != 11）。
补登记之后现测：两条的 `Name` 已按既有形态带上「（已下架）」、`Description` 前缀
「【已下架：改了不生效，改动期间请勿依赖】」，该用例改名为 `TestRetiredSetMatchesTheRetiredList`
（不再把条数写进函数名——9 → 11 → 13 漂了两次），`-test.v` 单跑 5 枚下架相关用例全 PASS。

每条都带 `deprecated_reason`，写清「为什么接不上」与「要接回来得先做什么」，
用户在配置页打开「显示已下架」开关就能看到原因与下架时间，且无法编辑。
原委逐条见 §7.2 的表格。读路径仍能读到它们（`ListByGroup(…, includeDeprecated=true)`
只用于 service 内部缓存加载），管理端默认看不到（`include_deprecated=0`）。
写入侧返回 `ErrParamDeprecated` 而不是静默成功。

### 9.5 阶段六的归属与接手的三条红灯

阶段六的 28 个代码文件 + 2 份文档，在我做完干净检出验证、正准备提交时，
被一次并行的 RBAC 提交（`64b46dd1`，commit message 是「user-web 菜单权限 +
角色菜单分配 + 收件箱数据权限」）一并带走——**第三次被他人提交夹带**
（前两次：`86b92349` 的 confidence 批次、`40a19abb` 的 2d 生产代码）。
逐文件核对内容完整、双远端已含，故不改写历史，只在此登记。

同一提交还把两处冲突带进了 master，本轮一并修掉：

| 红灯 | 真因（不是绕过测试） | 处置 |
|---|---|---|
| `/api/system/menus` 重复注册，整个 `internal/router` 包起不来 | 新 `menu_routes.go:25` 与既有 `frontend_aliases.go:442` 的 `doReg` 撞同一路径，`doReg` 对已注册路径 panic | 删旧别名 + 删 `SystemMenus` 方法。旧桩返回 `[]any{}` 落到前端 `treeRes?.tree \|\| treeRes?.data?.tree` 的 `undefined` ⇒ **菜单分配对话框一个节点都渲染不出来，RBAC 接上了但管理员无法给任何角色分配菜单** |
| G6 判 4 个 `t()` 键解析不到词条 | 同一提交里 `RoleList.vue` 用 `t('key', '中文')` 形式，中文在 fallback 位 ⇒ 界面正常但 8 种语言全回落中文 | 给 9 种语言各补 4 个键，门绿。fallback 参数保留（词条缺失时的兜底） |
| 两处反向钉死测试未跟上下架改动 | `lock.ai_lock_ttl` 从「未接线」改判「已下架」，测试还在断言前者 | 跟着改成断言「已下架」+ `d.Deprecated == true`，并反向断言不含「未接线」；`TestAILockTTLStaysUnwired` 重命名为 `TestAILockTTLStaysRetired` |

纪律复盘：「验证通过后立刻提交」连续第三次失效。暂存区是共享的，
跨会话等待提交就等于把成果交到别人手里让人替你背书。

修完上面三条之后又冒出来一批**与本任务无关**的前端失败：同一棵树连跑四次分别是
0 / 7 / 30 / 11 个失败，每次落在不同文件，单跑全绿。真因是用例普遍拿
`await flushPromises()`（只排空微任务）当「等渲染稳定」，而组件的防抖搜索、
轮询、重试退避全靠 `setTimeout`。这类随机红灯会阻塞 CI，详见
`CONFIG_DB_DRIVEN_PLAN.md` §6.10（新建 `tests/unit/settle-helper.js` 的
`settle()` + `vitest.config.js` 串行化）。**不记在配置治理的账上**——
它既不是硬编码也不是僵尸参数，是仓库本来就有的测试基础设施债，
只是这轮收尾时被负载放大了才浮出来。

### 9.6 复查「所有数据项都能在 UI 配置」挖出的四件事（2026-10-12）

阶段六做完了参数页与字典页之后，才回头认真读任务书里那句「所有数据项在 UI 页面允许用户配置」。这一读又挖出四件事——**两件是产品缺口，两件是既有缺陷**，都不是硬编码，但都属于「债务遗留」的范畴。

| # | 事 | 真因 | 处置 |
|---|---|---|---|
| 1 | 功能开关建得出却启不了、调不了灰度、删不掉 | `internal/controller/feature_flag.go` 的 Update/Delete/Enable/Disable/Rollout 五个动作**签名齐全但零路由可达**，`flagAdmin` 组只挂了 Create。`Evaluate` 于是永远走 `not_found` 返回 false | 补挂 5 条 `PUT/DELETE /:id` + `POST /:id/{enable,disable,rollout}`；`RegisterCodeReference` 不挂（零调用方，挂了等于开一个没有消费者的写端点） |
| 2 | 唯一的只读参数没有写为什么不可改 | 217 条种子里 `ReadOnly: true` 只有 `knowledge.embedding_dimension` 一条，Description 只写「bge-m3 默认 1024」。UI 有徽标但没写**后果** | Description 改写为：改它必须重建全部向量与索引；维度与存量不一致时检索全是噪声，**而且错得很安静**（不报错，只是答非所问） |
| 3 | 我自己的两个配置页「路由能进、菜单点不到」 | 菜单在他人 RBAC 提交后改为后端驱动（`internal/model/menu.go` 的 `SystemMenuTree` + `GET /api/menus`）。只加前端路由不会自动进菜单 | 在 `dynamicThreshold` 之后并列加 `systemDictionaries` / `systemFeatureFlags` 两条 |
| 4 | 15 个菜单项一直静默显示成兜底图标 | `Layout.vue:25` 直接下标 `iconComponents[menu.icon]` 且**无兜底**，而 `SubMenuItem.vue` 走有兜底的 `resolveRouteIcon`；`routeIconMap` 未登记 13 个名字，其中 `Cloud`/`QrCode`/`Shield` 在 `@element-plus/icons-vue` 里根本不存在 | `Layout.vue` 统一走 `resolveRouteIcon`；`iconMap.js` 补登记 12 个；`menu.go` 三处换成有效图标（`QrCode→Postcard`、`Cloud→Box`、`Shield→Lock`） |

**（5）第四件事牵出第五件：一条"看起来在守、其实在漏"的判据。**

新写的 `menuRegistry_routes.test.js` 抓出 (3)(4) 之后，还抓出了我自己刚写的 `menuRegistry-helper.js`：它用 `NODE_RE = /\{([^{}]*)\}/g` 解析 `menu.go`，**跨不过嵌套花括号**，于是所有写成一行且带 `Roles: []string{"admin"}` 的菜单项（含 `dynamicThreshold`、`systemDictionaries`、`systemFeatureFlags`）被静默丢弃——170 个 `Path:` 字面量只解析出 139 个。

后果比 bug 本身恶劣：**「菜单 path 都能在前端路由里找到」那条断言，因为列表本身少了几条而显示通过。** 改成逐行取 `Key/Title/Icon/Path`（都是 `字段: "字面量"` 形态），并加自检「解析条数必须等于 `Path:` 字面量个数」，不等直接抛错。

解析器修好后立刻又冒出 2 处「父子同 path」（`/browser-automation/tasks`、`/geo-tools/keyword-mining`）。核实后确认**不是真重复**：分组节点自己也带 `Path` 指向默认子页，点分组名进默认页、点子项进具体页，是有意设计；是我的断言没区分分组与叶子。

**教训（这条值得单独立账）**：那道门第一次跑是绿的，我拆掉一个菜单项做破坏验证，它正确报红；**可把菜单项还原之后它又红了**——暴露的是上面那个 helper bug。
⇒ **门能通过不代表门在守，必须做一次破坏验证。** 这个习惯此前只用在对 shell 脚本门禁的怀疑上（"以为自己修好了，其实没修好"），这次证明它对测试同样成立，也是唯一能抓到"静默漏"的办法。

**最终状态**：功能开关页 `FeatureFlags.vue` + `api/featureFlags.js` 新建（开关二次确认、失败后回读服务端真实状态、删除与停用的文案区分；编辑保存时因 `FlagUpdateRequest` 没有 `Enabled` 字段而额外调一次 enable/disable）。详见 `CONFIG_DB_DRIVEN_PLAN.md` §6.11。
