# Bug Verification: Lead-mining v2 ReAct runtime is installed but not operational

- **Slug**: `lead-mining-v2-react-runtime-gap-20261125`
- **Tested**: 2026-11-25
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

原 assessment 列出的 ReAct 生产缺口在 scoped自动化复现中已不再出现：真实 dispatcher 的 NoFC/NativeFC 请求均为零原生 Tools，Action 被执行并回灌 Observation，frozen scope/tenant/alias/raw fallback/降级持久化均通过真实 PostgreSQL 与本地 fake HTTP provider 验证。

14:48 的全包回归已在外部编译稳定后完整执行：repository 全包 PASS，router 为 4 个既有顶层失败，service 为 20 个既有顶层失败；ReAct 失败事件为 0。与 12:10 service 基线逐名比较，current-only=0、common=20、baseline-only=6（六项 lead-mining 用例由失败转为通过）；router 基线/当前失败集合完全一致。

但 18:13 的哈希复核发现 `lead_mining.go`、`lead_judge_context.go`、`lead_mining_judgement.go` 在 14:48 后又被外部工作改动，因此该全包结果不能关闭当前工作树回归。本报告保持 `partial`，待外部 build 停止后重跑 repository/router/service，再与冻结基线逐项比较。

## Main Agent Re-audit（2026-11-25 18:13 +08:00）

- 完整读取 17 MB 原始日志并独立解析：全部 test event PASS=9253 / FAIL=90 / SKIP=12；命名顶层 PASS=6407 / FAIL=24 / SKIP=12。包事件为 repository PASS=1733、router PASS=421/FAIL=5/SKIP=2、service PASS=7099/FAIL=85/SKIP=10；FAIL 包含子测试事件，不能与命名顶层数混用。
- 4 个 router 失败与 20 个 service 失败均已在 `check-lead-react-released-full-final-main-audit.txt` 完整展开；`FAILED_REACT_TEST_EVENTS=0`。
- service 基线 26 项与当前 20 项逐名比较：common=20、current-only=0、baseline-only=6；六项转绿为 `TestLeadMining_Integration_{AsyncEnqueue,Debounce,FullPipeline}` 与 `TestProcess_{Debounce,DedupAcrossTime,LeadDetected}`。router 基线/当前失败集合均为同 4 项。证据：`lead-react-full-final-main-baseline-reconciliation.txt`。
- isolated no-ReAct 工作树归因：68 个变更/未跟踪文件中 62 个与当前工作树字节一致；6 个差异文件均分类为后续外部/ReAct 改动，未发现 ReAct 覆盖孤立工作树文件。Router 在孤立树被构造器恢复为 HEAD，其非 ReAct 字节与当前一致，故 router/SQL 归因仍代表 ReAct 变化。
- 当前 ReAct 专用 task/resolver/dispatcher 文件与 Router 哈希仍匹配本报告；三个共享文件已漂移，必须以新哈希跑一次当前工作树全包回归，旧哈希结论不得验证新哈希。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Text-only production RED | `check-lead-react-text-only-red-r2.log` | fail (expected) | 旧路径仍发送 3 个 Tools、prompt 缺任务/scope、Action Input 被 Trim 破坏 |
| Strict-args RED | `check-lead-react-strict-args-red-r1.log` | fail (expected) | malformed/scope override/empty query 仍调用 resolver；dictionary 仍在白名单 |
| Boundary RED | `check-lead-react-boundary-red-r1.log` | fail (expected) | nil DB、全量状态计数、empty resolver 直接 Final Answer 三项失败 |
| History-scope adaptation RED | `check-lead-react-adaptation-red-r3.log`、`check-lead-react-adaptation-tenant-red-r1.log` | fail (expected) | canonical alias 欠查、raw Customer360 泄漏、missing task 重建、missing/SystemScope fail-open |
| Boundary GREEN | `check-lead-react-boundary-green-r3.log` | pass | 3/3，rc=0 |
| Repository adaptation GREEN | `check-lead-react-repository-adaptation-green-r2.log` | pass | 1/1，rc=0 |
| Service adaptation GREEN | `check-lead-react-service-adaptation-green-r2.log` | pass | 26 top-level + 9 subtests，rc=0 |
| Final scoped contract | `go test -p 1 -parallel 1 ./internal/service ./internal/repository -run '^(TestLeadReact\|TestBuildLeadReactTask\|TestNewLeadReactResolver\|TestInstallLeadReactDispatcher)' -count=1 -v` | pass | 31 top-level + 15 subtests，0 FAIL/SKIP，rc=0 |
| Repeated serial matrix | 同上加 `-count=3` | pass | 93 top-level + 45 subtests，0 FAIL/SKIP，rc=0 |
| Repository full | `go test -p 1 -parallel 1 -json ./internal/repository ./internal/router ./internal/service -count=1`（14:48 完整运行） | repository PASS；router/service baseline FAIL | repository 1733 test events PASS；router 4 个命名顶层失败；service 20 个命名顶层失败；ReAct 失败事件 0 |
| Full-service baseline comparison | 基线 `check-crm-empty-industry-service-full-final.log` vs 14:48 当前日志 | pass（该时点无新增失败） | service common=20、current-only=0、baseline-only=6；router 基线/当前 4 项集合相同 |
| Current-worktree rerun | 外部 build 停止后重跑同命令 | pending | 18:13 发现三个共享文件哈希在 14:48 后漂移；14:48 结果不能关闭当前工作树 |
| Build | `go build ./...` | pass before drift / blocked after | 14:38:19 rc=0；14:41:49 被外部 `crm_customer.go` 类型错误阻断 |
| Vet | `go vet ./...` | pass before drift | 14:38:32 rc=0 |
| Layer boundary | `python3 scripts/check-layer-boundary.py` | pass | 1764 files / 0 violations |
| Architecture | `bash scripts/check-architecture.sh` | pass | 仅既有 `wecomCSService` warning |
| GORM-only | `bash user-server/scripts/check-gorm-only.sh` | pass | 0 `.Exec(` / `.Raw(` 业务命中 |
| Naked goroutine | `bash user-server/scripts/check_naked_goroutine.sh` | baseline fail | 62 处既有命中；ReAct scoped hits=0，未扩修 |
| Diff check | `git diff --check` / `git diff --cached --check` | pass | rc=0，无输出 |

所有 PostgreSQL 测试均通过 `run-user-db-check-cwd.sh`，使用 `-p 1 -parallel 1`；scoped matrix 为 0 SKIP。未启动浏览器、未发真实业务外发、未部署。

## Output Excerpts

```text
check-lead-react-service-contract-final-r2.log:
ok  hivemtk-user/internal/service  2.195s
ok  hivemtk-user/internal/repository 0.782s
rc=0

check-lead-react-serial-matrix-r3.log:
ok  hivemtk-user/internal/service  1.764s
ok  hivemtk-user/internal/repository 0.688s
rc=0
# 93 top-level PASS + 45 subtests PASS，0 FAIL/SKIP

check-lead-react-affected-full-final.log:
ok  hivemtk-user/internal/repository 56.413s
internal/service/crm_customer.go:8:2: "strconv" imported and not used
internal/service/crm_customer.go:11:2: "hivemtk-user/internal/pkg/tenant" imported and not used
FAIL hivemtk-user/internal/router [build failed]
FAIL hivemtk-user/internal/service [build failed]
rc=1

lead-react-build-after-external-crm-drift.log:
internal/service/crm_customer.go:121:21: cannot use final.OwnerUserID (variable of type uint) as *uint value in assignment
rc=1
```

## Provenance and Stability

HEAD 始终为：

```text
2c5c53e3ded742ac0c6ebb1944529aaed18973fd
```

外部 monitor118 基线与本 lane 合并后六共享文件哈希：

```text
09876f0454b3dd6ee9c93089a574a16a452083373f1eeaf6724fbe2bd40f5c09  user-server/internal/service/lead_history_scope.go
e4a54e23911e1ceae8d43ee6b988640ca5ed315cf3975295913b5cb14f8cd0f9  user-server/internal/service/lead_history_scope_test.go
071b6c040bb16231bc52b06506251c76d810497501741d4ec66d458698e4fb7c  user-server/internal/service/lead_judge_context.go
094ba9bbfe46973ace2e9d5c02767999277f5e897982106b570782f49d929bfc  external monitor118 lead_mining.go
a0ad6d4c74bcc94e0ec7eeab3612ce640cf3df96237f0aa489857cf833ae43b3  merged lead_mining.go
35f1ff7f00c69b5f677fed98afa646d2985e995c6f4145d27518b524e504aa71  user-server/internal/repository/message_hub_inbox.go
79a513d079961c8c0722190951dfe775f554cc40aa455ba9d6cebe688188f381  user-server/internal/repository/message_hub_history_scope_test.go
```

`lead_mining.go` 的 provenance 证明见 `fix.md`：monitor118 重建件哈希为 `094ba9bb…`，与当前合并文件 diff 只有 frozen-task 注入一行。

ReAct scoped 文件最终哈希：

```text
95f0e51fdb07a8635040faebc958ac9a03132057baab80d54b23e7828829bfdc  user-server/internal/service/lead_react.go
6855321d363725c4d529247a66910882f2db8645c31888f83e67585afa129230  user-server/internal/service/lead_react_task.go
add16611515b7fb5d8072f6fb3a7fb9265e801c11cfeeedbc86bbabdfd9ab965  user-server/internal/service/lead_react_resolver.go
9a424270b1b383d6ab1bae57b174f71963177e78fb0c8c8fe84cdaaebef7465e  user-server/internal/service/dispatcher_judge.go
d287473b3a868fde0ce554c3d1d9ebb57c4af91a00219dc5c5e7e8b341c87d97  user-server/internal/service/lead_react_test.go
f6d6fe3b69381aedfde410c2828c74cb152f17c91715c0f910c129a31892ecb7  user-server/internal/service/lead_react_resolver_test.go
b3a0d4355c7ce0b2bb4c901f110bdc690bb0d2dce4e484e69db9f983dec1468a  user-server/internal/service/dispatcher_judge_test.go
e00364b8da08ae0ece7010cb875cbba867f31c1741ce2e521b79ea444933c33d  user-server/internal/repository/lead_react_scope_test.go
db0c3048e1d334d75e8356f68610b11d5ae8aa0aeed90a39a7ecf43c68c1816f  user-server/internal/repository/customer_session.go
6ffbc232e94d186e2795d09f90314d5d957736fbd041b98e91164108a2f868bd  user-server/internal/repository/lead_mining_judgement.go
495f12685e9fd111d1ff0ba25cc1e27d295672c66e9a4b80e4e166d1d93ca981  user-server/internal/router/router.go
```

外部阻塞文件未被本 lane 修改；其内容在验证期间持续变化，最后一次只读观察为 `crm_customer.go` SHA256 `a25a65125d8192868545bfe4c0134e980f5ccd87424bb1593a3456bb08291e16`、mtime `2026-11-25T14:43:56+0800`。该文件在 monitor126 后再次变化，按指令只报告并停止全包重跑；该观察不构成稳定声明。

## Residual Risks

- 14:48 时点全包已执行且无 ReAct/新增失败，但 18:13 发现三个共享文件哈希漂移；当前工作树仍需新鲜全包回归，不能沿用旧哈希结论。
- Agent persona/provider/model/version 注入按批准范围延后。
- 未做真实 UI、线上 dispatcher、真实外发或部署验证。
- naked-goroutine 门仍有 62 处 HEAD 既有基线命中，不在本 lane 扩修。

## Recommendation

ReAct scoped 实现与 RED→GREEN 证据成立；14:48 全包运行证明该时点无 ReAct 失败、无新增顶层失败。最终 bug verdict 暂保持 `partial`：待外部 build 停止且共享文件稳定后，以 18:13 新哈希重跑 repository/router/service 全包并完成基线逐项比较，再决定是否升级为 `verified`。
