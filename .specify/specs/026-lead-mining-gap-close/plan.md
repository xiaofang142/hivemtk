# Implementation Plan: 线索发掘模块缺口收口

**Input**: `.specify/specs/026-lead-mining-gap-close/spec.md`

## 技术上下文

- 后端 Go + Gin + GORM，五层 Router→Controller→Service→Repository→Model；无新表。
- 前端 Vue3 + Element Plus；API 层 named import `{ http }`；路由按域拆 `src/router/modules/`。
- 所有改动复用既有字段/接口，不引入迁移、不引入外部网络调用。

## 实施步骤

### Phase 1 — 统计维度扩展（FR-001/002）

1. `internal/repository/clue.go`：新增 `GetClueStatisticsWithRange(ctx, start, end int64)`，
   复用 `GetLeadMiningStats` 的六维聚合逻辑，外层套 `create_time BETWEEN` 过滤；
   保留 `GetClueStatistics` 原签名（兼容既有调用）。
2. `internal/service/clue.go`：`GetClueStatistics` 接收 start/end 并校验 start<=end；
   非法返 `ErrInvalidStatsRange`（在 `internal/domain/errors/codes.go` 登记业务码或用 400 传输码）。
3. `internal/dto/clue.go`：新增 `GetClueStatisticsRequest{Start, End int64}`。
4. `internal/controller/clue.go`：`GetClueStatistics` 绑定 query，非法参数 `response.Error(400, ...)`，
   成功回 `response.Success`。
5. `internal/repository/clue_test.go` / `internal/service/clue_test.go`（或等价合成测试）：
   夹具覆盖跨时间窗、角色/渠道/单群聊维度、非法参数。

### Phase 2 — 群聊富化上下文进判定提示（FR-003）

6. `internal/service/lead_judge_context.go`：在 `buildLeadJudgeContext` 组装 `jctx.Messages` 后，
   若 `hub.IsGroup` 且 `ChannelAccountID>0`，调用 `ConversationContextService.AggregateGroupContext`
   取富化消息与角色计数；成功则替换 `jctx.Messages` 为富化消息并在 `LeadJudgeContext` 增加
   `GroupRoleCounts map[string]int` 字段；`buildLeadJudgeSystemPrompt` 输出
   「群成员发言统计」行。
7. 降级：聚合失败保留原始消息并写 `logger.Warn`，提示中追加「群聊上下文聚合失败，证据不完整」。
8. 测试：`lead_judge_context.go` 的提示构造单测，断言富化前缀与角色计数行、单聊无角色行、降级有标记。

### Phase 3 — 评分看板（FR-004）

9. `user-web/src/api/clue.js`：补 `listGrades` / `scoreClue` / `scoreAll` / `getScore` 四个请求函数。
10. `user-web/src/views/clue/ScoreBoard.vue`：评分看板页（等级分布卡片 + 列表 + 单条重评分 +
    批量评分），按规范加唯一根 class `clue-score-board-page` 与区域 class。
11. `user-web/src/router/modules/clue.js`：追加 `/clue/score-board` 路由。
12. `user-web/src/router-list.md`（仓根 + user-web 各一份）：追加路由台账行。
13. `user-web/src/i18n/locales/{zh,en}.json`：补评分看板文案 + `onboarding.page.clue-score-board` 四步。
14. `user-web/src/composables/useOnboarding.ts`：`getPageTourSteps` 添加 `clue-score-board`。
15. `user-web/tests/unit/`：补评分看板组件单测（vi.mock api，断言三动作）。

### Phase 4 — 台账收口（FR-006）

16. `.specify/custom-task-crm-lead-channel-20261124/tasks.md`：把 T001–T071 全改成 `[x]`，
    并保持其他内容不变（纯状态同步）。

### Phase 5 — 验证与提交

17. `go build ./...`、`go vet ./...`、`bash scripts/check-architecture.sh`、
    `bash user-server/scripts/check_naked_goroutine.sh`、`bash user-server/scripts/check-gorm-only.sh`。
18. Go 测试（显式 `POSTGRES_TEST_PASSWORD`，否则记 not-run）：`go test ./internal/service/... ./internal/repository/...`。
19. `cd user-web && npm run test && npm run lint:check`。
20. 全部绿后一次性 commit + push 双远端（gitee-upstream / upstream，先 fetch 确认 fast-forward）。

## 风险与纪律

- 不碰其他 lane 的在途文件；工作区当前干净（仅 `.mimosa/` 未跟踪，属扫描工具产物，不入库）。
- 统计接口新增参数必须向后兼容（不传 start/end = 原行为）。
- 群聊聚合必须 fail-visible，失败不可静默吞。
- 新页面必须同步 i18n 双语 + 页面级新手引导，否则按未完成处理。
- UI 真机点验按规则 -1 逐元素显式操作；如环境不可用，如实记 not-run。
