# Implementation Plan: ego-lite 运行时监督与收口杂项

**Feature**: `015-egolite-runtime-supervision` ｜ **Created**: 2026-10-28 ｜ **Spec**: `./spec.md`

## 技术路线（一句话）

把「运行时收敛」拆成三层可落地件：**执行前静扫**（拦已知恶意模式，扫描门镜像）、
**域守卫 helpers**（给脚本一条被审计的导航通路）、**事件 URL 域审计**
（强制层：越域事件 ⇒ error + kill）；升级指令复用既有 WS 下行帧面（零新协议）。

## 域监督的纵深结构

```
发布前        静态扫描门 EGO-R001..R031（服务端，双端同源规则库）
执行前        runnerEntryBlockers 二次静扫（runner 侧镜像，拦篡改/脏数据）
执行中-协作   __gotoChecked__/__fetchChecked__（脚本自愿走受审计通路，越域抛错）
执行中-强制   auditEventURL：事件 payload.url 越域 ⇒ error 事件 + cancel（fail-close）
审计兜底      ego_event_log append-only + Monitor 时间线（自述级 G-E5 口径不变）
```

诚实边界：ego 内嵌 Node 的 `require`/`import()` 无法从 runner 进程拦截——
完整 JS 沙箱需 ego 官方能力面配合；本三层拦的是「已知恶意模式 + 越域导航可见面」。

## 设计决策

| # | 决策 | 理由 |
|---|---|---|
| D1 | 二次静扫只扫 entry 原文，不扫 preamble | preamble 是 runner 自产受信代码（confirm 契约合法使用 `import("node:fs/promises")`）；误杀自己=腿死 |
| D2 | 域判定单一语义三处实现（Go `domainAllowed` / JS `__domainAllowed__`） | 双端同源靠语义注释 + 向量测试钉死（`ego-91` ⋢ `ego-9` 教训的同类处理） |
| D3 | 零域单跳过事件审计 | 服务端保存时 domains 必填是权威面；审计层只对「已声明」的白名单执法，避免误杀历史资产 |
| D4 | `www.` 归一 + 子域命中 + `*` 整站 | 与 flow 保存时「禁部分通配」规则互补；钓鱼域（`evil-github.com`）靠 `.` 边界隔离 |
| D5 | upgrade 广播返回送达数，结果靠 runner 事件回传 | 升级是长时操作（官方 CLI 自带确认/重试），服务端不做同步等待 |
| D6 | EGO-R032 是 warn 不拦发布 | 软契约最低要求：一行事件都没有的资产审计只剩退出码，但不应一票否决（人工审核清单消化） |
| D7 | deny 收口不使用 process.exit | EGO-R013 禁止 process 面；且 exit(0) 会伪造 done 终态——终态唯一权威是服务端状态机 |
| D8 | LaunchAgent KeepAlive + conf 不覆盖 | runner 崩溃自动拉起（断线退避已在进程内）；轮换 token 走服务端撤销重签，避免安装器覆盖手工配置 |

## 分层落点

| 层 | 文件 | 改动 |
|----|------|------|
| 服务端 | `internal/egolite/execution_service.go` | dispatch 下发 `domains`（draft/market 两分支）+ `parseDomains` |
| 服务端 | `internal/egolite/runner_registry.go` | `BroadcastUpgrade()` |
| 服务端 | `internal/router/egolite_routes.go` | `POST /admin/runner/upgrade` |
| 服务端 | `internal/egolite/scan.go` | EGO-R032 warn（eventPrintPattern） |
| runner | `cmd/ego-runner/main.go` | taskSpec.Domains；preamble `__DOMAINS__` + 域守卫 JS；`runnerEntryBlockers`/`firstEntryBlocker`；`domainAllowed`/`auditEventURL`；execEgo 统一 `handleEvent`（补 seq→透传→审计→越域 cancel）；`runUpgrade`/`egoUpgradeArgs`；readLoop `upgrade` 帧 |
| runner 测试 | `cmd/ego-runner/runtime_guard_test.go`（新） | 预检 7 恶意全拦/良性零误伤；域判定 9 例+3 边界；审计 4 类；注入契约；组装拒绝；upgrade argv |
| 安装器 | `user-server/scripts/ego-runner/{install.sh,uninstall.sh,README.md}`（新） | macOS 一键安装/自检/卸载 |
| 前端 | `ego/Flows.vue`、`ego/Assets.vue`、`EgoRunnerInstallGuide.vue` | 契约提示 ×2 + conf 模板/install.sh/官方升级说明 |
| 服务端测试 | `internal/egolite/flow_service_test.go` | parseDomains 3 例 + EGO-R032 2 例 |

## 风险与回退

- 事件域审计只看 payload.url：脚本不给 url 就审不到（自述级审计的既有边界，G-E5 口径）。
- 守卫 helpers 是协作层：裸 `page.goto` 仍可达（ego 原语面决定）；强制层依赖事件流暴露 url。
- 回退：三层各自独立，任何一层误伤都可单独关闭（静扫删表项、审计清空 Domains、
  helpers 脚本不调用即不存在）。
