# Tasks: 线索发掘模块缺口收口

**Input**: `.specify/specs/026-lead-mining-gap-close/spec.md` + `plan.md`

## Phase 1: 统计维度扩展

- [x] T001 扩展 `ClueRepository`：新增 `GetClueStatisticsWithRange(ctx, start, end)`，六维聚合 + 时间窗过滤，保留原方法
- [x] T002 `ClueService.GetClueStatistics(ctx, start, end)` 加 start<=end 校验，非法返 400 错误常量
- [x] T003 `dto/clue.go` 加 `GetClueStatisticsRequest`，`controller/clue.go` 绑定 query + 错误响应
- [x] T004 后端单测：时间窗 + 六维聚合 + 非法参数 + 无数据空数组

## Phase 2: 群聊富化上下文

- [x] T005 `LeadJudgeContext` 加 `GroupRoleCounts`，`buildLeadJudgeContext` 群聊分支调用 `AggregateGroupContext` 替换消息
- [x] T006 `buildLeadJudgeSystemPrompt` 输出富化消息 + 群成员发言统计；聚合失败明确标记证据不完整
- [x] T007 单测：群聊富化前缀/计数、单聊无角色行、降级标记

## Phase 3: 统计页接服务端区间聚合

- [x] T008 `src/api/clue.js` 补 `statisticsRange(start, end)`（有区间时调用）
- [x] T009 `src/views/clue/Statistics.vue` 区间刷新改调服务端区间统计接口，渲染六维；
  保留无区间时的既有行为；不再静默截断
- [x] T010 前端单测：区间刷新打到 `/clues/statistics/range`、六维渲染、错误不吞

## Phase 4: 台账收口

- [x] T013 `.specify/custom-task-crm-lead-channel-20261124/tasks.md` T001–T071 全部 `[x]`

## Phase 5: 验证与提交

- [x] T014 Go 门禁：build / vet / check-architecture / naked-goroutine / gorm-only
- [x] T015 Go 测试（显式 POSTGRES_TEST_PASSWORD；否则记录 not-run）
- [x] T016 前端 `npm run test` + `npm run lint:check`
- [x] T017 UI 逐元素点验（规则 -1；不可用记 not-run）
- [x] T018 整体完成一次性 commit + push 双远端（fetch 确认 fast-forward）
