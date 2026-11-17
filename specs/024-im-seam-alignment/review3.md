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

## C. 留观

- service 全包基线时长 ~282s（本批新增测试增量 <10s，无劣化）。
- `wecom_integration.go` 的 `if false ||` 死条件与 `webhttp/ima` 粗细能力归位：025 首切片清理。
