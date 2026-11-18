# 024 二次审核（三）：三任务收口批对抗复核

> 复核对象：三任务收口批——任务一（45 任务验收逐条核对 + 34 条点名测试补齐）、
> 任务二（全功能测试）、以及其间的缺陷修复。
> 复核方式：换镜头找漏洞，全部直读代码/实测取证。
> 结论先行：**一处 B2 产码真缺陷（已修）+ 一处我方夹具根因（已修）+ 一处存量前端缺口（已修）**，
> 其余为无行动确认项。

---

## A. 已修复

### R1 B2 产码真缺陷：`MessageHub` 模型缺 `updated_at` 字段（阻断级——新装库编辑覆盖必坏）

- **事实**：生产库 `message_hub` 有 `updated_at timestamptz DEFAULT now()` 列（实测
  `information_schema.columns`），T202 的 `UpdateContentByChannelMsgID` 显式写该列；
  但 `model.MessageHub` 缺 `UpdatedAt` 字段 ⇒ AutoMigrate 建的库（测试库/全新安装）没有这一列，
  编辑事件第一次覆盖即 `42703 column "updated_at" does not exist`。
- **修复**：模型补 `UpdatedAt time.Time gorm:"autoUpdateTime"`。生产列已存在 ⇒ 部署 AutoMigrate
  无 schema 变更（no-op）；新装库与模型对齐。模型迁移门 `check_model_migration.py` 绿。
- **教训**：B2 交付的仓储方法没有配验收命名测试（本次任务一审计发现 34 条缺失的根源之一），
  缺测试 ⇒ 缺陷在生产潜伏。本批 34 条补齐即补的这道防线。

### R2 我方夹具根因：`evidenceEventMine` 只把 `kind` 用在 EventID，未设 `EventType`

- **事实**：初版验收夹具 `EventType` 为空 ⇒ NormalizeEvent 归一成 message.received ⇒
  事件走进消息管线（落行/去重/拦截），平台事件分支从未被执行——撤回/成员/交互测试
  全部在测错误的东西。探针定位：`res.reason="intercepted by middleware: duplicate(same
  msg_id ingested in another conversation…)"`、`eventType="message.received"`。
- **修复**：夹具显式 `EventType: kind`；断言改为读 seed 行（Order id asc）。
- **教训**：分流类测试的夹具必须自带类型，且断言要读「被作用的那一行」而不是 Order desc
  取末行（消息管线可能插行）。

### R3 存量前端缺口：`traceObservation*` i18n 键缺失（非 024 范围，全绿目标内修复）

- **事实**：`TraceMonitor.vue` 引用 `traceObservationSkipped`/`traceObservationResultExit`，
  zh/en 两语言均无此键——`traceMonitor.test.js`（已提交）恒红，全量 vitest 951/952。
- **修复**：zh（跳过 / AI 结果出口）与 en（Skipped / AI result exit）补键，diff 仅 2 行/文件。
  验证：vitest 全量 **97 文件 / 952 用例全绿**；eslint exit 0。
- **归属**：TraceMonitor 属 L 轮批次（63290cbe）引入的组件，键缺失为该批遗留，非 024 改动。

### R4 我方夹具时序：bridge/whatsapp 验收测试的 svc/建库顺序与二次建库

- **事实**：①bridge 三测试 `svc := NewWebhookService()` 先于 `testutil.NewTestDB` ——
  仓储构造时捕获**旧全局句柄**，占位/重试行写进错误（空）库；②whatsapp 夹具内部二次
  `senderTestDB`，把用例先种的数据断链到另一个库。
- **修复**：建库先行（svc 之后所有落库走同一句柄）；夹具改为接收外部 DB。

---

## B. 无行动确认项

1. **模型补字段的生产等价性**：生产列 `timestamptz DEFAULT now()`；模型 `autoUpdateTime`
   部署 AutoMigrate 无变更（列已存在）。`go vet ./...` 绿；模型迁移门绿。
2. **QQ 被动窗第 6 条语义**：原实现不本地降级（seq 持续单调 1→6，msg_id 恒携带，限额由平台
   执法）——与搬迁前逐字节一致，T112 golden 与单测双钉。
3. **`-race` 子集**（金样/注册表/新验收 24+ 测试）绿；包内无 `t.Parallel()`，全局替换缝安全。
4. **T606 验收**：`npm run build` 绿（precache 653 entries）；页面逐元素点验需登录态浏览器，
   按规则 -1 留给人工步（构建/单测/eslint 三道机器门已过）。
5. **外来 3 条**：`TestIMEventEvidence_*`（并行会话 untracked 文件）与本批改动无关（基线同红），
   归其作者会话；本批模型补字段不改其失败模式（pgx 编码问题，另有根因）。

---

# 四审追加（2026-11-18 第二轮：「一次性完成所有未完成」批）

## D. 已修复（全部实测取证）

### D1 阻断级：MODE=on 下 varchar 租户列全断（隔离 callback 恒绑 uint）

- **事实**：B2 证据链表（channel_event_audits / channel_interactions）租户键**刻意**用字符串
  （recordRouteDiscard 头注释明示设计意图），但租户隔离 callback（tenancyidx T005）
  `clauseWhereTenant` 恒绑 uint —— MODE=on 下读/写这两张表 pgx 直接
  `cannot find encode plan`（外来 3 条测试的真实根因，与 geo_* 事故同族）。
- **修复（保持 B2 设计，不动模型）**：登记表内省**按列类型**扩展（data_type 入登记），
  新增 `IsStringTenantTable`；`tenantBindValueFor` 按「登记列类型 + 模型字段类型」双保险
  绑定字符串形态；WHERE 注入 / JOIN 内联（带引号字面量）/ Create 填充（含 map 路径）
  全链类型感知；`normalizeTenantIDColumns` 加豁免清单（两表是「人工决定保留字符串键」
  的口径，转 bigint 会砸设计）。
- **验证**：tenancyidx / db 包测试绿；外来 3 条全绿（首次）；service 全量回归绿（291s）。

### D2 证据链三处产码缺陷（T209 契约升级，全部按外来测试契约修）

1. **租户权威**：审计/交互行租户键写的是 Extra 自报值 —— 改为 **ctx 权威**（tenantEventKey：
   ctx 绑定 > Extra 兜底），伪租户不再生效。
2. **跨账号去重丢证据**：幂等键缺 account_id —— 同 msg_id 在两账号下只留一份。
   修：两表唯一键入 account_id（模型 tag + **迁移 070** 幂等重建索引，生产已部署的
   068/069 索引由 070 重建）。
3. **半证据**：交互登记与审计分两次独立写，故障时留「有点击没审计」的半链。
   修：`RecordCallbackWithAudit` 同事务（Repository 层事务，符合五层架构）；
   任一故障 **err 透传 + Accepted=false**（证据故障必须明确拒绝；平台重投由幂等键兜底）。
   审计写失败口径同步升级：静默放行 → 拒绝（handlePlatformEvent 全分支）。

### D3 T501 模型 tag 漂移（阻断级——新装库身份锚形状错）

- **事实**：`model.CustomerChannel` 的 `idx_channel_identity` tag **漏 channel 列**、
  业务键名未带 v2 —— AutoMigrate 新装库身份锚少一列，迁移 065 的 `IF NOT EXISTS`
  静默跳过修复；pg_indexes 直查当场暴露（列序断言的牙齿）。
- **修复**：tag 与 065 逐列对齐（含 065 §4 的 RENAME 终名口径：v2 → idx_oneid_channel）。
  另发现迁移测试的执行坑：CONCURRENTLY 在测试连接下静默失败 —— 测试执行剥离
  CONCURRENTLY（幂等/形状验收不需要免锁），生产路径不受影响。

### D4 E：`wecom_integration.go` `if false ||` 死条件清理（三审留观项，本轮清零）。

### D5 C：T205 矩阵扩全表：qq/feishu/telegram 三具名腿 + 其余渠道「Events≤received-only」
全表兜底断言（与 channelcaps_events_test 互补）。

### D6 D：T606 收口状态

- 机器验证全覆盖：`npm run build` 绿、vitest **新增 3 例组件级点验**
  （筛选选项 + snoozed 徽标 + 待首响徽标条件渲染）、eslint exit 0。
- 浏览器逐元素点验**环境受阻**：本地无归因登录态全栈（18080 实例连 5433 跨项目库，
  显示未初始化向导——不可归因即不可越权初始化）。已按组件级断言留证，人工步补浏览器侧。

## E. 复核过程事故与恢复（记录）

- 三审 stash 归因实验触发了 `autostash` 残留（裹并行会话工作区状态），pop 冲突卡半途——
  已 `git reset` 复原树状态（.specify 删除/.env-example/website 等与原状一致），
  stash@{0} 留档未删；期间前端全量 vitest 的 17 文件红为**并行会话在制 i18n 重构区**
  （uncommitted：i18n/index.js、locale.js + untracked catalog.js/traceTiming.js），
  与 024 改动无关（归因实验：stash 我的键改动后同红；本批足迹 5 文件 14 用例单独跑全绿）。

## F. 无行动确认项

1. 生产 `channel_event_audits/interactions` 现存 varchar 列 **保持 varchar**（设计口径），
   070 只重建索引不动列型；部署 AutoMigrate 对该两表无列型变更诉求（模型 string 与列 varchar 一致）。
2. normalize 豁免清单若未来新增字符串租户键表，必须同步在此登记（文件内注释已写明）。

- service 全包基线时长 ~282s（本批新增测试增量 <10s，无劣化）。
- `wecom_integration.go` 的 `if false ||` 死条件与 `webhttp/ima` 粗细能力归位：025 首切片清理。
