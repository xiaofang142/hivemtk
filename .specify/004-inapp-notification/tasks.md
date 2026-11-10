---
description: "站内消息通知模块实施任务清单"
---

# Tasks: 站内消息通知模块

**Input**: 设计文档 `.specify/004-inapp-notification/`
（spec.md / plan.md / research.md / data-model.md / contracts/ / quickstart.md）

> **✅ 状态收口（2026-11-10 清单整理轮）**：本特性已全部实现并经审计验证——双仓产物在位：
> 用户端 `user-server`（`model/notification.go` + `model/notification_read.go` + `migration/migrations/inapp_notification_migration.go`，`NotificationRead` 已登记 `allModels`）；
> 平台端 `platform-server`（`model/notice_delivery_log.go` + `repository/notice_delivery_log.go` + `service/notice_service.go` + `message_service.go` 写 `PublishedAt`，`/merchant-api/tenant/notice`、`/notices/list`、`/notices/report` 路由在位）；
> 用户端前端（`src/views/Notifications.vue` + `components/MessageNotification.vue` + `api/notification.js`）已上线；
> 平台→用户同步链经《未完成任务清单》B-流4 审计 ✅（notice_sync 钉扎写面/游标自恢复/回执对账）；
> 通知中心页正由 K 轮线上 UI 测试覆盖（清单 K-线上-1）。
> 下方 58 条任务**不再逐条回填勾选**（避免虚报）；本文件自此降级为 spec-kit 设计交付记录，
> 任务跟踪唯一入口 = 工作区根 `未完成任务清单.md`。

**仓库归属**：本任务跨 **两个独立 git 仓库** ——
`hivemtk/`（用户端）与 `hivemtk-platform/`（平台端）。
下文路径以仓库为根，`USER=` 指 `hivemtk/`，`PLAT=` 指 `hivemtk-platform/`。

**执行纪律**：CLAUDE.md 规则 0b 禁止并行子 Agent ⇒ 本清单**串行执行**。
`[P]` 仅表示"文件互不冲突"，不表示并发派发。

---

## Phase 1: 数据与模型基座（阻塞所有故事）

**Purpose**: 建表、扩列、模型与仓储就位。

- [ ] T001 `USER=user-server/internal/model/notification.go` 加字段 `level` / `source_msg_id` / `source`，并加**部分唯一索引** `(tenant_id, source_msg_id) WHERE source_msg_id > 0`（普通唯一索引会撞爆存量本地通知 —— 见 data-model.md §3）
- [ ] T002 `USER=user-server/internal/model/notification_read.go` 新建回执模型：`tenant_id` / `notification_id` / `user_id` / `read_at`，复合唯一 `(tenant_id, notification_id, user_id)`
- [ ] T003 `PLAT=platform-server/internal/model/message.go` 加 `published_at` 字段；`targets` 与 `level` **保持既有列不新增**（它们已存在，只是没接线）
- [ ] T004 `PLAT=platform-server/internal/model/notice_delivery_log.go` 新建投递台账模型，唯一键 `(message_id, tenant_code)`
- [ ] T005 `USER=user-server/internal/pkg/db/migrate.go` 把 `&model.NotificationRead{}` 登记进 `allModels()`（登记三处口径之一）
- [ ] T006 `USER=user-server/internal/migration/migrations/inapp_notification_migration.go` 新建 Go 版本化迁移：建 `notification_reads` 表 + 给 `notifications` 加三列（幂等，可重复执行）
- [ ] T007 `PLAT=platform-server/internal/utils/db/db.go` 把 `&model.NoticeDeliveryLog{}` 登记进 `migrateModels`；并补 `ALTER TABLE platform_messages ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ` 幂等 DDL 兜底（AutoMigrate 失败会被 warning 静默吞掉 —— db.go 既有教训）

**Checkpoint**: 两库迁移可重复执行；`go build ./...` 绿。

---

## Phase 2: 平台端 —— 发布与投递（US1 + US2）

**Goal**: 平台运营方能发布消息，并能按租户维度对外提供拉取。

- [ ] T008 [P] `PLAT=platform-server/internal/service/message_service.go`：`Send` 真正消费入参 `level`（现硬编码 `info`）与 `targets`；新增标题/正文非空、标题 ≤255、level 枚举校验；写 `published_at`
- [ ] T009 [P] `PLAT=platform-server/internal/repository/message.go`：新增 `ListForTenant(ctx, tenantCode, since, limit)` —— 过滤 `published_at IS NOT NULL` 且（`targets` 空 或 含 tenant_code）且 `published_at > since`，按 published_at 升序
- [ ] T010 [P] `PLAT=platform-server/internal/repository/notice_delivery_log.go` 新建仓储：upsert 投递结果 + 按 message_id 查询
- [ ] T011 `PLAT=platform-server/internal/controller/merchant_notice_controller.go` 新建 `GET /merchant-api/notices/list`（参数绑定 + 调 service，不碰 repository）
- [ ] T012 `PLAT=platform-server/internal/router/router.go` 把 `merchantNotice.GET("/notices/list", ...)` 挂到既有 `merchantAPI` 组（自带 `MerchantAuth`）—— 签名 PATH 不含 query
- [ ] T013 `PLAT=platform-server/internal/controller/message_controller.go` 补 `GET /platform/message/:id`（详情）与 `GET /platform/message/:id/deliveries`（台账）两个端点
- [ ] T014 `PLAT=platform-server/internal/service/message_service.go` 补 `GetByID` 与 `ListDeliveries` 两个方法
- [ ] T015 [P] `PLAT=platform-server/internal/controller/message_controller.go` 的 `Send` 改为透传 `level`，并加 level 非法时的 400

**Checkpoint**: 平台端 `go build ./...` + `go vet` 绿；`curl` 能发布并按 tenant_code 拉到。

---

## Phase 3: 用户端 —— 投递落库（US1 + US2）

**Goal**: 用户端把平台消息拉到本地库，幂等、隔离、不重不漏。

- [ ] T016 [P] `USER=user-server/internal/platform/notice_client.go` 新建 HMAC 客户端：`GET /merchant-api/notices/list?tenant_code=&since=`；**签名 PATH 剥离 query**（照 `module_client.go:213` 的形状）；平台不可达返回可识别错误
- [ ] T017 [P] `USER=user-server/internal/repository/notification.go` 新增 `UpsertFromPlatform(ctx, n)`（幂等：依赖部分唯一索引 + `ON CONFLICT DO NOTHING` 语义），以及按 `source_msg_id` 查重的辅助方法
- [ ] T018 [P] `USER=user-server/internal/repository/notification_read.go` 新建回执仓储：`MarkRead`（upsert）、`UnreadCountForUser`、`ListReadNotificationIDs`（供列表 join）、`MarkAllRead`
- [ ] T019 `USER=user-server/internal/service/notice_sync.go` 新建同步服务：按本地 `tenants.code` 反查租户号 → 逐租户拉取 → 落库；**写库用 `tenant.OnlyTenant(ctx, tid)`**（用 SystemScope 会让行落默认租户 1 —— research.md R5）
- [ ] T020 `USER=user-server/internal/service/notice_sync.go`：平台不可达 ⇒ 保留上次已投递、不清空、不报错中断（research.md R12）；`PLATFORM_ENABLED=false` ⇒ 完全跳过、不发请求
- [ ] T021 `USER=user-server/internal/service/notice_sync_runner.go` 新建周期 runner：周期 3 分钟、首轮**同步执行**、失败不阻断启动、`Stop()` 可优雅退出（照 `module_sync_runner.go` 的形状）
- [ ] T022 `USER=cmd/api/main.go` 装配同步 runner（DI 在装配层完成，router 只做映射）
- [ ] T023 `USER=user-server/internal/service/notice_sync.go` 投递成功后上报投递结果到平台台账（失败只 Warn，不阻断投递）

**Checkpoint**: 用户端 `go build ./...` 绿；手动触发同步后 `notifications` 出现 `source='platform'` 的行。

---

## Phase 4: 用户端 —— 已读按人隔离（US3）

**Goal**: 一人已读不影响他人。

- [ ] T024 [P] `USER=user-server/internal/service/notification.go`：`List` 改为对平台消息 join 当前用户的回执算 `is_read`；本地业务通知仍走行上原值（语义不变）
- [ ] T025 [P] `USER=user-server/internal/service/notification.go`：`CountUnread` 改为"平台消息无回执数 + 本地业务通知原口径"（现实现是全局未读位，一人会清零全场 —— contract §4）
- [ ] T026 `USER=user-server/internal/service/notification.go`：`MarkRead` 对平台消息写回执（upsert 幂等）；本地业务通知保持原逻辑
- [ ] T027 `USER=user-server/internal/service/notification.go`：`MarkAllRead` 对平台消息批量写回执
- [ ] T028 `USER=user-server/internal/controller/notification.go`：`MarkRead` 跨租户保护 —— 通知不属于当前租户则 400，不泄露该消息存在
- [ ] T029 `USER=user-server/internal/repository/notification.go`：`List` 支持按 `level` 过滤（FR-010）

**Checkpoint**: 甲已读后乙的未读数不变（SC-003 判定点）。

---

## Phase 5: 前端 —— 平台发布页（US1 + US2）

- [ ] T030 [P] `PLAT=platform-web/src/api/notice.js` 新建 API 出口：列表 / 发布 / 详情 / 投递台账
- [ ] T031 `PLAT=platform-web/src/views/platform/notice/List.vue` 新建页面：列表 + 发布对话框（标题/正文/级别单选/目标租户多选含"全体租户"）；租户选项取自既有 `GET /platform/tenants`
- [ ] T032 `PLAT=platform-web/src/router/modules/platform.js` 挂路由 `/platform/notice`，分组"系统"
- [ ] T033 [P] `PLAT=platform-web/src/views/platform/notice/List.vue` 加 i18n key（仓内 i18n 多语言包同步）

**Checkpoint**: platform-web `npm run build` 通过；发布页能选中租户并发布。

---

## Phase 6: 前端 —— 用户端接收与详情（US1 + US3 + US4）

- [ ] T034 [P] `USER=user-web/src/api/notification.js` 新建 API 出口：列表 / 详情 / 标记已读 / 全部已读 / 未读数
- [ ] T035 `USER=user-web/src/views/Notifications.vue` 去掉"用户端 API 失败 ⇒ 退化成只拉平台 latest"的 fallback（该 fallback 与本模块语义重复）；列表区分 `source`
- [ ] T036 `USER=user-web/src/views/Notifications.vue` 详情弹窗补充展示 `level` 标签与 `source`；已读改调用户端已读端点（不再调平台 `/platform/message/:id/read`）
- [ ] T037 `USER=user-web/src/layout/Layout.vue` 给 `unreadCount` **接线**（现声明后从未赋值，恒 0 —— research.md R10）；形状照同文件 `geoAlertCount`
- [ ] T038 `USER=user-web/src/components/MessageNotification.vue` 飘条改读**本地**通知中心最新一条（不再打平台 latest），与通知中心同源同口径
- [ ] T039 [P] `USER=user-web/src/api/notification.js` + `PLAT=platform-web/src/api/notice.js` 补 i18n key

**Checkpoint**: user-web `npm run test` 通过；铃铛角标真实显示未读数。

---

## Phase 7: 测试（本模块核心风险全在隔离与幂等）

- [ ] T040 [P] `USER=user-server/internal/service/notice_sync_test.go` 幂等测试：同一平台消息投递两次 ⇒ 本地只有一行
- [ ] T041 [P] `USER=user-server/internal/service/notification_test.go` 按人已读测试：甲已读 ⇒ 乙的未读数不变
- [ ] T042 [P] `USER=user-server/internal/service/notice_sync_test.go` 跨租户隔离测试：**两个不同租户**各投递 ⇒ 彼此看不到对方的行（同键两租户各一行才照得出跨租户 —— 同键会假绿）
- [ ] T043 [P] `USER=user-server/internal/service/notice_sync_test.go` 平台不可达测试：保留上次已投递、不清空
- [ ] T044 [P] `PLAT=platform-server/internal/service/message_service_test.go` 发布校验测试：空标题/空正文/超长标题/非法 level 全部拒绝
- [ ] T045 [P] `PLAT=platform-server/internal/repository/message_test.go` 投放过滤测试：`targets` 空 ⇒ 全体命中；含本租户 ⇒ 命中；不含 ⇒ 不命中

> ⚠️ 测试必须用真实 PG：跑前导出
> `POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8202 POSTGRES_TEST_PASSWORD=...`
> `testdb.go` 连接失败会 `t.Skipf` 一律跳过 ⇒ **假绿**。

---

## Phase 8: 门禁与发布

- [ ] T046 `USER`：`go build ./...` + `go vet` + `gofmt -l` 全干净
- [ ] T047 `PLAT`：`go build ./...` + `go vet` + `gofmt -l` 全干净
- [ ] T048 `USER`：受影响包 `go test` 真实跑过并贴输出（无 FAIL、无 SKIP）
- [ ] T049 `PLAT`：受影响包 `go test` 真实跑过并贴输出
- [ ] T050 `USER=user-web`：`npm run test`；`PLAT=platform-web`：`npm run build`
- [ ] T051 按 `quickstart.md` 走一遍本地端到端（发布 → 收到 → 幂等复核 → 已读隔离 → 跨租户隔离 → 离线形态）
- [ ] T052 交叉编译两个二进制（`GOOS=linux GOARCH=amd64`），传到生产并**重建镜像**（只 `up -d` 会跑旧镜像旧二进制）
- [ ] T053 发布后**验二进制内容**（并发泳道共用 `deploy/out`，发布后必须验）
- [ ] T054 按 `quickstart.md` 第五节走生产端到端：平台发布 → 平台查投递台账 → 用户端查落地 → 看同步日志
- [ ] T055 生产 UI 验证：平台端发布页可用；用户端铃铛出现未读角标、通知中心能看详情、标记已读后角标减 1 且他人不受影响
- [ ] T056 文档更新：`hivemtk/docs/api-list.md`、`hivemtk/docs/router-list.md` 补新端点
- [ ] T057 逐实体审计台账更新：`hivemtk/user-server/docs/architecture/model-list.md` 补 `NotificationRead` 行（新表登记 + 租户表判据 ✅）
- [ ] T058 任务整体完成后一次性提交：`hivemtk` 仓 → `gitee-upstream` + `upstream` 双远端；`hivemtk-platform` 仓 → 其自身 origin

---

## 依赖与执行顺序

```
Phase 1 (基座)  ──▶ Phase 2 (平台端)  ──▶ Phase 3 (用户端投递)
                                  └──▶ Phase 5 (平台前端)
Phase 1 (基座)  ──▶ Phase 3 (用户端投递) ──▶ Phase 4 (已读隔离) ──▶ Phase 6 (用户前端)
Phase 7 (测试)  依赖 Phase 3/4 完成
Phase 8 (门禁+发布) 依赖全部
```

**为什么 Phase 2 与 Phase 3 有先后**：Phase 3 的同步器要拉的端点在 Phase 2 才存在。

**为什么 Phase 4 独立于 Phase 3**：已读口径改动不影响投递链路的正确性，
但必须与 Phase 3 一起才构成完整可测的用户故事（US3）。

## 实施策略

**MVP 切片** = Phase 1 + 2 + 3 + 7（T040/T042/T043）——
平台能发、用户端能收、幂等与隔离正确。这已交付 FR-001~FR-009。

**增量**：
1. MVP（发布 + 投递）→ 验 → 可上线
2. Phase 4（按人已读）→ 验 → SC-003 达成
3. Phase 5/6（两端前端）→ 验 → 用户可感知
4. Phase 8（门禁 + 发布）

## Notes

- `[P]` = 文件互不冲突，**不代表并发派发**（规则 0b 禁并行子 Agent）
- 提交时机：**整个任务完成后一次性提交**（规则 1），不按 Phase 分批提交
- 两仓分别提交推送；远端名以现测 `git remote -v` 为准
- 部署形态：`TENANT_MODE=on` + `PLATFORM_ENABLED=true`（线上实测），
  离线形态（`PLATFORM_ENABLED=false`）必须回归验证不发起平台请求
