# JEV 融合任务计划（步骤四→五）

> 目标：给 browser-automation 增加 JEV 毫秒级决策层。Brain（大 LLM）保留做规划/文本/验收；步级"选哪个操作/哪个元素"下沉 JEV choice。
> 基线文档：docs/browser-automation/full_link_research_step1.md（本系统无 JEV）/ computer_use_jev_research_step2.md / jev_ultrafast_research_step3.md
> 设计文档（步骤四）：docs/browser-automation/jev_integration_design_step4.md
> 铁律：默认关闭（BROWSER_JEV_ENABLED 未置位时零行为变化）；JEV 只提名不直发不可逆动作；D7/写台账/审计/重试闸门全部保留并包住 JEV。

## Phase 1 — 步骤四：设计文档 [complete]
- 写 docs/browser-automation/jev_integration_design_step4.md：架构、链路、问题单、验收口径。

## Phase 2 — 步骤五：实现 JEV 决策层 [complete]
- `user-server/internal/browser_automation/service/jev.go`（新建）：
- 8 项单测全绿；`go vet` 干净；`gofmt` 干净。
  - JevConfig（env：BROWSER_JEV_ENABLED/ENDPOINT/API_KEY/MODEL，默认关）
  - parseSnapshotElements（解析 `role "name" @eN` 行，上限 250）
  - buildJevState + fingerprintState（sha256）
  - JevClient.Choose（POST System-One，timeout 25s，429/529/503 退避 0.5*2^n×3）
  - validateChoice（5 项：choice∈ids / probs键==ids / 值∈[0,1]有限 / sum≈1±0.02 / choice==argmax）
  - decisionToSteps（CLICK→click @eN / SCROLL_DOWN/UP→scroll / WAIT→wait / DONE→done / BLOCKED→中止；未知 operation 拒绝）
- `executor.go` 最小接线：executeBrain 轮内 snapshot 后先走 planRound（JEV 优先、失败回退 Brain）；judge 帧 `jev_decision` 审计。
- 不碰：dto 白名单、D7、台账、hand、host_registry、扩展 JS（v1 复用现有 @eN 快照）。

## Phase 3 — 步骤五：测试与门禁 [complete]
- `service/jev_test.go`：解析/校验5项/映射/回退/fake-server 端到端 choose。
- `go vet ./internal/browser_automation/...` + `go test ./internal/browser_automation/service/ -count=1` 全绿。
- 反向验证：摘掉回退 → 无配置时应走 Brain（用 fake brain 断言）。

## Phase 4 — 提交推送 [pending]
- `git add` 仅本批路径 → commit → 直推 gitee-upstream + upstream（规则0/1）。

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| （待填） | | |
