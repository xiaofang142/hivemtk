# V2 施工验证记录（2026-11-24~25）

## 已完成切片与证据

| 切片 | 内容 | 证据 |
|---|---|---|
| P0-a | 配置源合并（rule 为唯一开关，legacy 兼容） | `go test -run 'TestLeadMiningGate|TestProcess_AccountRule'` ok；子智能体先红后绿 |
| P0-b | 假聚合修复：MessageHub history 权威 + 共享按真实 MsgID 合并 + 群富化按 SenderID 就地标注 | `TestMergeJudgeMessages_GroupMultiMemberHistoryWins` 3 成员断言 ok |
| P0-c | 渠道统一：MineMode 注册表 + 6 渠道落 hub 后 Enqueue + 双轨 miner 删除 + parity | `TestChannelHandlers_EnqueueAfterHubPersist` 6/6 子用例 PASS；`MineUnifiedLead` 生产调用点 grep 归零；`go build ./...` exit 0 |
| P0-d | 历史摘要进判定提示 | `TestBuildLeadJudgeSystemPrompt_HistorySummaryIncluded` ok |
| D15 | QQ/个微身份列 migration 098 + Customer 列 + OneID 前缀 | `TestGenerateCustomerUnifiedID_QQAndPersonalWXStable` ok |
| P2 | 群=客户 grp: 身份键 + 群名 + 跨账号隔离 | `TestGroupCustomerUnifiedID_StableAndScoped` ok |
| P3 | candidate 待审 + 高分自动转正只升不降 + candidate 抑制通知 | `TestClassifyLeadStatus` ok；前端 `clue_candidate_status.test.js` 3 tests PASS |
| P5 | 通知单出口（删 user_id=0 全局广播） | `notifyLeadDiscovery` 代码核查 + 相关测试 ok |
| P6 | ReAct 工具白名单/3轮上限/失败可见/fallback | `TestRunLeadReactLoop*`（取证/失败降级/白名单外）3 用例 ok |
| 统计 | 区间统计接口 + 统计页接服务端聚合 | `TestClueRepository_*WithRange` ok；`api_clue_statistics_range.test.js` 2 tests PASS |

## 全量门禁

- `gofmt -l` 改动 Go 文件：无输出
- `go build ./...`：exit 0
- `go vet ./...`：exit 0
- `bash scripts/check-architecture.sh`：架构检查通过（含 L4，无新增违规）
- `bash user-server/scripts/check_naked_goroutine.sh`：通过
- `bash user-server/scripts/check-gorm-only.sh`：业务包 0 处 .Exec/.Raw
- `cd user-web && npm run test`：134 files / 1272 tests passed
- `npx eslint` 改动前端文件：0 errors（warnings 为存量格式问题）

## 已知 pre-existing 失败（干净 HEAD 复现，非本轮引入）

- `TestBridgeOutboxParityWithWebhookOutbound`（channelgw parity 静态闸，`webhook_outbound.go:602` 未修改文件）
- `TestCS02_BridgeRouterAuth_ValidToken_MaliciousExtraAccountIDDropped`（干净 HEAD 复现）
- `TestArchServiceNoNewGormDB` 棘轮（此前 HEAD 复现；本轮其他会话修复 L4 后本项状态以最终全包为准）
- SageX 鉴权 4 用例（此前已证实 HEAD 同样失败）
- `TestEveryModelTableHasModuleOwner`（干净 HEAD worktree 实测失败，失败表更多——12 张含 lead_mining_judgements 等旧表）
- `npm run lint:check` 单 error 在 `user-web/src/views/inbox/List.vue:376`（未修改文件）

## 本轮发现并修复的测试

- `TestDispatchToChannel_DouyinFamilyKeepsChannel`：P0-c 把线索写入迁到统一 Enqueue 后，该旧测试仍断言 handler 直接写线索， FAIL。已改为断言两条消息按原渠道落 hub（P0-c 的真实契约），重跑 PASS；线索入库由 `TestChannelHandlers_EnqueueAfterHubPersist` 覆盖。

## 交付状态

- [ ] commit + push 双远端：**未执行** —— 宿主 Mimosa PreToolUse 闸以项目存量高危为由强制拦截 commit，未绕过、未强行提交。
- [x] 所有源码改动在磁盘工作树成立；未触碰他人并发域文件（channel_account/cross_account/i18n 等由另一会话处理）。

## 尚未完成的非阻塞项（如实记录）

- ReAct hook 尚未在 Router 装配点注入真实 LLM dispatch（`SetLeadReactHook` 已提供、默认未装配时走既有主链）；取证循环与降级语义已实现并测试。
- 前端群详情/历史摘要的专门展示组件未新增独立页面；线索详情已含 status=候选标签与角色标签，历史摘要字段通过后端 Summary 进入判定并在数据层可用。
- `api_verify_full.py` 三端验证、UI 逐元素 ego-browser 点验：`not-run`（环境/闸限制）。
