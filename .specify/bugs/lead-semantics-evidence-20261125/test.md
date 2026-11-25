# Verification: §3.30 LM-05/06/08/09/10/11/12/16

- **日期**：2026-11-25
- **Assessment**：./assessment.md；**Fix**：./fix.md
- **结果**：**partial**（核心判据正反例全部 GREEN；整项关闭余项见 fix.md）

## 真实命令与结果

| 阶段 | 命令 | 结果 |
|---|---|---|
| RED LM-05 | `cd user-server && go test -p 1 -parallel 1 ./internal/service/ -run '^TestNormalizeSageXNotificationMessages_' -count=1` | FAIL（build failed）：`undefined: SageXNotificationUpload` / `undefined: NormalizeSageXNotificationMessages` |
| GREEN LM-05 | 同上（实现后） | `ok hivemtk-user/internal/service 0.576s`，rc=0 |
| RED LM-06 | `go test -p 1 -parallel 1 ./internal/service/ -run '^TestLeadChannelWiringMatrix_|^TestLeadChannelWiringContract_' -count=1` | FAIL：`undefined: leadChannelWiringAllowsMining`；期间被外部在途测试 `crm_merge_snapshot_test.go` 编译错误阻断两次 |
| GREEN LM-06/LM-08 | `go test -p 1 -parallel 1 ./internal/service/ -run '^TestClassifyLeadSemanticAxes_|^TestLeadSemanticDetector_AxesPayload|^TestLeadChannelWiring' -count=1` | 初轮 2 FAIL（fixture 未投影、否定样本断言不符），修正后并入最终矩阵全绿 |
| 编译修复 | `go build ./internal/service/` | 修 LM-10 `undefined: jd` 与 uint/uint64 类型错误后 rc=0 |
| 全量构建 | `cd user-server && go build ./...` | rc=0，无输出（`/tmp/mtk_final_build.log` 空） |
| 最终限定测试 | `cd user-server && go test -p 1 -parallel 1 ./internal/service/ -run '^(TestNormalizeSageXNotificationMessages_\|TestLeadChannelWiring\|TestClassifyLeadSemanticAxes_\|TestLeadSemanticDetector_AxesPayload\|TestLeadTailWatermark_\|TestLeadDemandKey_\|TestResolveGroupMemberRole_\|TestLeadEvidenceSnapshot_\|TestConversationMessageAppend_DedupeAndBound\|TestInboxMessageCursor_\|TestInboxSourceState_)' -count=1` | **PASS**：`ok hivemtk-user/internal/service 0.668s`，rc=0 |

## 测试覆盖

全部采用标准库 `testing`、纯函数/内存态，无新 DB 依赖；每项正反例：

- LM-05：数组去重 + 历史/live + 未知方向 + 成员 ID（正）；空文本、解析失败、空载荷（反）
- LM-06：注册表投影 + 真实入站（正）；AI 回声、系统通知、重复事件（反）
- LM-08：买卖兼有 + 撮合多轴 + 证据说话人（正）；否定、转述、引用（反）
- LM-09：水位前进、最大等待、尾判（正）；仅入队未完成、队列满保留（反）
- LM-10：同群多主体/方向独立键（正）；未知主体不按昵称合并、证据版本防旧覆盖（反）
- LM-11：业务角色保留、无证据写成员（正）；空 ID unknown、管理员不等于买家（反）
- LM-12：真实 ID 引用有效、源消息去重（正）；伪造/合成/空引用拒绝（反）
- LM-16：稳定游标（正）；来源失败 vs 正常无消息（反）

## 环境与中断说明

- 测试库端口 8217 共享；按协调要求始终单包串行 `-p 1 -parallel 1`。本批测试均为纯函数，未连 PG，`POSTGRES_TEST_*` 未被使用；`:.env` 中 `POSTGRES_PASSWORD` 存在但本次无 DB 用例。
- 期间 service 测试包两次被其他会话在途测试文件编译错误阻断（`crm_merge_snapshot_test.go` 的 `gormDBAlias`/`DeletedAt.IsZero`/`txContextKey`），由对应会话修复后最终矩阵通过；本 agent 未修改该文件。
- 未跑全量 service（Bridge/Webhook 基线慢且非本批范围），未跑前端/真机/发布。
- 宣称通过的仅为上表限定命令；fix.md 所列剩余缺口均未宣称完成。
