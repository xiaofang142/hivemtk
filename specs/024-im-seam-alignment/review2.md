# 024 二次审核（二）：收口批对抗复核

> 复核对象：2026-11-18 收口批（B1 测试补齐 + B6 全批 + 存量缺陷修复，即 delivery.md §二–§四）。
> 复核方式：换镜头找漏洞——正确性 / 失效模式 / 可验证 / 可交付，全部直读代码与实测取证。
> 结论先行：**三处需修（全部已修）+ 一处复核过程当场暴露的流程漏洞（已修）+ 若干无行动确认项**。

---

## A. 已修复（规则 2：发现问题立即修复）

### R1 金样样例不是自包含的（严重——可验证性失效）

- **事实**：feishu/api_err、wecom/api_err、wechat/api_err 依赖 ok_text 先跑暖化 token 缓存
  （feishu/wecom 缓存在 DB 行 `access_token`，wechat 在 per-service `tokenClients` map）；
  qq 三个样例共用同一 msg_id，`msg_seq` 跨样例递增（api_err body 里 seq=2）。
  ⇒ 单独 `-run 'TestOutboundBehaviorFreezeGolden/feishu/api_err'` 必红（调用数不一致），
  「单样例可独立验收」不成立。
- **修复**：① svc 改为**每样例独立**（隔离 tokenClients/msgSeq 等内存缓存）；
  ② feishu/wecom 每样例清 DB token 缓存（强制 fetch → 每条样例都捕获 token 调用）；
  ③ qq msg_id 每样例唯一（msg_seq 恒 1）；④ 冻结比对改**按标签匹配**（子集 -run 也能校验子集）。
- **验证**：全量冻结绿；`feishu/api_err`、`wecom/api_err`、`wechat/api_err` 单独 `-run` 绿
  （每样例 2 调用含 token）；大回归集绿。

### R2 `TestSendOutbound_DelayedReplayNoRetry` 的「不入重试队」断言是空话（补强）

- **事实**：bare `&WebhookService{}` 的 delayedRepo 为 nil，重试无处可写——断言恒真，无牙。
- **修复**：改 DB-backed（testutil + NewWebhookService）+ **负对照**：非重放同款可重试失败
  入队 1 行（证明夹具有牙），重放同款失败必须仍为 1 行（0 新增）。绿。

### R3 触点脚本死代码（卫生）

- `model_channel_constants` 里第一段宽松正则循环的结果立即被严格循环覆盖——删除，保留单遍严格版。
  脚本输出不变（17/21/7/7/7/4）。

### R4 UPDATE_GOLDEN 部分覆写事故（流程漏洞，复核中当场触发）

- **事实**：复核者在带 `-run '…/feishu/api_err|…'` 过滤的 shell 里遗留 `UPDATE_GOLDEN=1`
  → golden 被截断成 4 条，靠 `goldenLoad` 的 `<24` 下限检查暴露。
- **根因**：重写口无护栏。
- **修复**：UPDATE 分支校验「样例集不全（run<cases）即拒绝重写」；另将比对改为按标签
  （见 R1④），使部分 `-run` 不再是「覆写」的唯一诱因。冻结文件已按全量重跑恢复 24 条。

---

## B. 无行动确认项（复核过，判定无需改动）

1. **R5 不变量安全性**：8 个 sender impl 逐文件复核「`!sent` ⇒ 必调 Fail/FailUntracked」全部成立
   （dingtalk 全分支 / feishu / telegram / qq / wecom / wechat / whatsapp / bridge 五分支）；
   不变量只会兜「实现者漏调」的未来缺陷，不会改变今天任何路径。
2. **`webhook_ai.go:206` 等忽略 sendOutbound 返回值的调用点**：R5/失败闭环收紧后，
   静默漏投变为可见失败+重试——符合 024「失败口径唯一真值」的意图，非回归。
3. **全局替换缝与并发**：本包无 `t.Parallel()`（async_db_handle_probe_test 头注释明示口径）；
   golden 的 Transport 路由表/记录器均有锁；`-race` 复跑绿。
4. **onboarding curl 路径**：`/api/webhook/<channel>/:account_id` 与 router/middleware 实际注册一致。
5. **错误类别现状**（whatsapp 131047→auth、feishu 230013→unknown+可重试）：按现状冻结，
   细化归 025 渠道错误分类整理（delivery §五已记）。

## C. 留观（记录不修）

- 触点脚本以正则提取源码符号，对格式漂移（如 `RegisterChannelSender(` 换行变形）敏感——
  CI 作业每次 push 全量跑，漂移即暴露；脚本头注释已声明口径。
- `wecom_integration.go` 的 `if false ||` 死条件：接缝范围外，025 平台批首切片清理。
