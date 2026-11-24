# 线索发掘 V2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按设计 v1.2 的 18 项决策，修复配置双轨/假聚合/渠道白名单，接入 OneID 与群客户身份，补待审队列、ReAct 工具取证、通知单出口，并验证交付。

**Architecture:** 遵循五层架构；MessageHub 为唯一消息事实源；Service.Enqueue 为唯一发掘入口；channelgw.Default 为渠道唯一真值源；OneID/CustomerIdentity 为客户身份唯一源。

**Tech Stack:** Go + Gin + GORM + Postgres；Vue3 + Element Plus；vitest。

---

## P0-a 配置源合并

### Task A1: 规则为发掘唯一开关
**Files:** `user-server/internal/service/lead_mining.go`, `lead_rule.go`, `_test.go`

- [ ] **Step 1**: 写失败测试：关闭 legacy enabled、配置账号级 LeadMiningRule(enabled=true)，投递消息，断言产出 judgement。
- [ ] **Step 2**: 跑测试确认 FAIL。
- [ ] **Step 3**: 在 `process` 中把 `cfg.Enabled`/`channelEnabled` 判据替换为 `LeadRuleService.Resolve` 结果的 enabled/channels/min_intent_score；legacy config 仅作默认值与兼容读。
- [ ] **Step 4**: 跑测试确认 PASS，既有 lead_mining 测试回归。
- [ ] **Step 5**: commit。

## P0-b 上下文覆盖修复

### Task B1: history 为权威，共享缓存只合并
**Files:** `user-server/internal/service/lead_judge_context.go`, `conversation_context.go`, `_test.go`

- [ ] **Step 1**: 真库测试：3 成员群聊分别发言，调 buildLeadJudgeContext，断言 jctx.Messages 含 3 个不同 SenderID 与原文。
- [ ] **Step 2**: 跑测试确认 FAIL（当前覆盖为 1 条）。
- [ ] **Step 3**: 删除 `lead_judge_context.go:211-218` 的覆盖逻辑；改为按 MsgID 去重合并共享缓存到 history；history 消息用真实 MsgID 替代 history_N。
- [ ] **Step 4**: ingress 层 `AppendMessage` 补真实双向历史（在 inbox_ingress_persist 落库后调用）。
- [ ] **Step 5**: 跑测试确认 PASS，单聊双向历史同样断言。
- [ ] **Step 6**: commit。

## P0-c 渠道统一

### Task C1: channelgw 能力位 + 全渠道 Enqueue
**Files:** `channelgw/registry.go`, `service/lead_miner*.go`, `bridge/*`, 各 `webhook_channel_*.go`, parity tests

- [ ] **Step 1**: 写集成测试：表驱动各渠道 webhook handler 投递 inbound，断言 lead_mining_judgements 新增。
- [ ] **Step 2**: 跑测试确认 FAIL。
- [ ] **Step 3**: ChannelSpec 加 MineMode，init 投影；删两处 leadMiningChannels map；channelEnabled 改默认全纳入。
- [ ] **Step 4**: 各渠道落 hub 后注入 Enqueue（统一在 InboxIngressService 出口）。
- [ ] **Step 5**: 删除 douyin_lead_miner/lead_miner_unified 及调用；parity 门扩写。
- [ ] **Step 6**: 测试全绿，commit。

## P0-d 摘要压缩

### Task D1: Summary 接线
**Files:** `lead_judge_context.go`, `conversation_context.go`, `_test.go`

- [ ] **Step 1-4**: TDD 把 ConversationContext.Summary 读进判定提示、判定后写回；超窗口 compaction；测试断言。

## P1/P2 身份与群客户

### Task E1: 新身份列迁移
- [ ] 迁移 + Identifiers 扩字段 + 归一规则 + 测试同 sender 稳定 unified_id。

### Task E2: 群=客户 + OneID
- [ ] resolveCustomer 群分支 grp: 键、群名、禁合并；单聊 IdentifyOrCreate；clue 键改作用域；CAS；测试。

## P3 待审队列

### Task F1: candidate 分档 + 证据校验 + 自动转正
- [ ] TDD 低分/证据不完整 status=candidate；真 MsgID 引用校验；高分自动转正写 status；通知抑制。

## P4 前端

### Task G1: 待审筛选/群详情/历史摘要展示
- [ ] api + 页面 + i18n 双语 + 引导 + 单测。

## P5 通知

### Task H1: 单出口收敛
- [ ] 删全局广播；candidate 抑制；群文案；脱敏与越权校验；测试外发次数=1。

## P6 ReAct

### Task I1: 工具式取证
- [ ] 工具白名单（查客户/历史线索/词库）、3 步上限、失败可见、fallback；指代消息测试。

## P7 交付

### Task J1: 全量验证
- [ ] go build/vet/gofmt/门禁/测试；npm test/lint；台账诚实性；一次性提交推双远端。

---

## 施工进度（2026-11-24 实时）

- [x] P0-b 假聚合修复：`mergeJudgeMessages` history 权威 + 共享缓存按真实 MsgID 合并；群富化就地标注不替换；3 成员回归测试 + 摘要接线测试（go test ok）
- [x] P0-d 历史摘要接线：`ConversationContext.Summary` 进判定提示 + 冲突裁决说明（go test ok）
- [x] D15 身份新列：migration 098 + customer.go QQ/个微列与前缀 + 同 sender 稳定性测试（go test ok）
- [x] P0-a 配置源合并：`leadMiningGateFor` rule 优先 + legacy 兼容 + 2 个新测试（子智能体先红后绿，go build ok）
- [x] P0-c 渠道统一（子智能体：MineMode 注册表 + 6 渠道 Enqueue 接线 + 旧 miner 双轨删除；测试全绿）
- [x] P2 群客户身份：grp: 键 + 群名 + 单聊 OneID + 跨账号隔离（go test ok）
- [x] P3 待审队列：classifyLeadStatus + promoteStatus 只升不降 + candidate 抑制通知（go test ok）
- [x] P5 通知单出口：去掉 user_id=0 全局广播（go test ok）
- [ ] P4 前端展示 + P6 ReAct
- [ ] P7 交付（受 Mimosa 提交闸 + 并发会话冲突限制）

**注**：每 Task 结束后必须 commit（writing-plans 纪律）；本项目规则1「整体完成才提交」与此冲突时，按用户本次「一次性全部完成」指令，采用每切片本地提交、最终一次性推送的折中，最终交付前汇总说明。
