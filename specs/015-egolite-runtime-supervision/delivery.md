# Delivery: ego-lite 运行时监督与收口杂项

**Feature**: `015-egolite-runtime-supervision` ｜ **Delivered**: 2026-10-28 ｜ **Spec**: `./spec.md`

## 结论

specs/014 改进清单 §三 C 组中**全部可代码化项**（C2 可实现子集 / C4 / C7-macOS / C10 /
C16 / C17）一次性交付；C1 完成「可自动化部分」（同名 space 跨进程续跑真机验证），
handOff 人工环节保持 P1 登记。契约冲突（`__awaitConfirm__` 依赖被扫描门禁止的
process.exit）一并清零。

## 改动文件

| 层 | 文件 | 内容 |
|---|---|---|
| 服务端 | `internal/egolite/execution_service.go` | dispatch 下发 `domains`（draft←flow.DomainsJSON / market←目录快照）+ `parseDomains`（坏 JSON/空 ⇒ nil=零域） |
| 服务端 | `internal/egolite/runner_registry.go` | `BroadcastUpgrade()`（全部在线 runner，返回送达数） |
| 服务端 | `internal/router/egolite_routes.go` | `POST /api/egolite/admin/runner/upgrade`（RequireAdminMiddleware） |
| 服务端 | `internal/egolite/scan.go` | EGO-R032 warn：全文无 `console.log(JSON.stringify(...))` ⇒ 事件流将为空 |
| runner | `cmd/ego-runner/main.go` | taskSpec.Domains；preamble `__DOMAINS__` + `__domainAllowed__/__gotoChecked__/__fetchChecked__`；`runnerEntryBlockers` 二次静扫（7 条镜像规则，只扫 entry）；`domainAllowed`/`auditEventURL`；execEgo 统一 `handleEvent`（越域 ⇒ error 事件 + cancel fail-close）；`runUpgrade`/`egoUpgradeArgs`；readLoop `upgrade` 帧 |
| runner 测试 | `cmd/ego-runner/runtime_guard_test.go`（新） | 6 组：预检 7 恶意全拦 + 良性零误伤；域判定 12 例（含钓鱼/前缀伪装/空域）；审计 4 类；注入契约；组装拒绝；upgrade argv |
| 安装器 | `user-server/scripts/ego-runner/{install.sh,uninstall.sh,README.md}`（新） | macOS 一键安装（编译→conf 不覆盖→LaunchAgent RunAtLoad+KeepAlive）+ `--check` 自检 + 卸载（conf/日志保留） |
| 前端 | `ego/Flows.vue` | 编辑器脚本契约 alert（常量/`__awaitConfirm__`/**不要 process.exit**/域守卫/事件输出） |
| 前端 | `ego/Assets.vue` | 运行面板契约 alert（写确认 + 域白名单口径） |
| 前端 | `EgoRunnerInstallGuide.vue` | conf 模板补 `ego_server_name` 可选行 + install.sh 指引 + 官方升级说明 |
| 服务端测试 | `internal/egolite/flow_service_test.go` | `TestParseDomains` / `TestScanWarnsOnNoEventOutput` |
| spec | `specs/015-egolite-runtime-supervision/`（新） | 四件套 |

## 检查命令与结果

```
go build ./...                                          → OK
POSTGRES_TEST_PORT=8202 POSTGRES_TEST_PASSWORD=<容器口令> \
  go test ./cmd/ego-runner/... ./internal/egolite/... \
          ./internal/model/... ./internal/router/...
  → 四包 ok（egolite 4.1s / router 22.2s）
npx eslint（Flows/Assets/EgoRunnerInstallGuide）        → 零 error 零 warning
bash -n install.sh uninstall.sh                          → OK
```

## 真机验证（ego lite 0.6.1.13）

同名 space 跨进程续跑（F12「Space 持久」支柱实测）：
round1 建 space（spaceId=10，example.com）→ round2 同名续跑得**同一 spaceId=10**、
p1 页面状态持久（URL 一致）→ `finish({keep:[]})` 正常收口。
handOff→takeOverTaskSpace 的人工确认环节需真人配合 ego UI，保持 P1 必测登记。

## 契约冲突修复

specs/014 的脚本契约让 deny 时 `process.exit(2)`，与扫描门 EGO-R013（process 进程面
禁止）冲突且会伪造 done 终态。修法：deny ⇒ 跳过写动作 + 打印 `ok:false` 审计行 +
正常结束——终态唯一权威是服务端状态机（deny/TTL 已判 stopped，done 帧只落审计行）。
preamble 注释与 specs/014 spec §五已同步。

## 遗留项（改进清单对应状态）

- C1 剩余：handOff→takeOverTaskSpace 全流程（需真人配合，P1）。
- C2 剩余：完整 JS 沙箱（require/import 运行时拦截）需 ego 官方能力面配合——长期项，
  当前三层（静扫/守卫/审计）为可实现上限。
- C3（法律评估，外部）/ C5/C6（platform 联动）/ C8/C9/C11/C13/C15/C18（按清单排期）不变。
