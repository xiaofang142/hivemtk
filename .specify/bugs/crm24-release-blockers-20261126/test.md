# Verification: CRM-24 release blockers

## RED

- Baseline reproduction confirmed the two bridge failures and SageX fixture panic without CRM-24 changes; the official deployment classifier returned `BLOCK`.
- `TestChannelResolver_IdentifyCodePinsChannelRepositoryTenant` then failed behaviorally for the unbound and old-system-tenant cases. The other boundaries were retained. Evidence: `crm24-blocker-resolver-red-serial-20261126.log` in the task's approved temporary directory.
- An earlier nonexistent test constant and an OS-killed compiler were corrected or rerun; neither is counted as behavioral RED.

## Focused GREEN — 2026-11-26

```sh
go test ./internal/service ./internal/bridge ./internal/controller \
  -run 'Test(ChannelResolver_|BindBridgeTenant_|BridgeTenantForRequest_|SageX)' \
  -count=1 -timeout=600s
```

- Exit code: `0`.
- Service: `1.700s`; bridge: `1.442s`; controller: `1.513s`.
- Dedicated PostgreSQL: `127.0.0.1:8217`, with existing credentials injected privately through the process environment. A private Go cache and serial package compilation avoided shared-cache and memory contention.
- Evidence: `crm24-blocker-focused-green-20261126.log` and `.exit` in the task's approved temporary directory.
- All tracked Go files satisfy gofmt. `crm24-blocker-format-verification-20261126.json` records the exact 52 baseline paths and verifies the 51 formatting-only changes.

## Final release gates

- `make audit`, `make fmt-check`, layer-boundary and architecture checks, both actual-value credential gates, and working/indexed whitespace checks completed with exit code `0`. The layer checker reported 1,787 production Go files and zero violations. Audit skips remain skips, including checks requiring unavailable coverage or sibling-repository inputs.
- The additional legacy `user-server/scripts/check_naked_goroutine.sh` returned 61 lexical matches in unchanged files. The authoritative checker selected by `make audit`, `python3 scripts/check-naked-goroutine.py`, passed with zero findings across 2,586 files; all seven checker self-tests passed. The legacy shell diagnostic lacks the current closure-aware recovery and registered exemption handling. No production code or baseline was changed to silence it.
- Static evidence: `crm24-final-static-gates-20261126.json`, its nine command logs, and `crm24-final-authoritative-goroutine-20261126.{log,json}` in the approved task temporary directory.
- A harness restart interrupted the full Go and serial frontend jobs. The interrupted make process is not a passing result. Its 32 completed full-suite package results are retained, excluding the authentication-only preflight; `go list ./...` enumerated 195 packages, leaving 163 to resume with the same database, count, serial execution and timeout settings. A fresh authentication preflight precedes the remainder. The source fingerprint and package manifest support the eventual combined coverage check.
- Frontend produced no completed test results before the restart. Its serial suite, lint and build will execute after the resumed Go suite, build, vet and lint to avoid competing memory-heavy processes. No commit, push, deployment or post-release acceptance is claimed while these checks remain unfinished.

## Full Go results and bridge fixture correction — 2026-11-26

- The resumed 163-package run finished at `12:00:22` with exit `1`: 162 packages passed or had no tests; only `internal/bridge` failed, in six existing top-level HTTP/acknowledgement cases. All tracked backend fingerprints remained unchanged through this run. Evidence: `crm24-full-go-red-before-fixture-fix-20261126.json` and `crm24-resumed-go-remaining-20261126.log`.
- Root cause: the mock server supplied one shared channel code for two seeded Douyin accounts. Production correctly rejects that ambiguous reference. The test fixture now supplies each account's existing unique account code, selected by canonical account ID; production code and all assertions remain unchanged.
- Full bridge GREEN: `go test ./internal/bridge -count=1 -p 1 -timeout 600s`, exit `0`, package time `22.488s`, finished `12:08:25`. Evidence: `crm24-bridge-fixture-green-20261126.{log,json}`.
- Combined coverage is `195/195` successful/no-test packages, with no missing or unresolved failed package. This combines 32 retained pre-restart results, the resumed run and the passing full bridge rerun. Exactly one bridge-local `_test.go` file changed afterward; the other 194 packages retain matching source fingerprints. This is not a claim that one uninterrupted `make test-go` invocation passed. Evidence: `crm24-combined-go-suite-20261126.json`.
- `go build ./...`, `make vet` and `make lint` completed with exit `0`; golangci-lint reported zero issues. Their logs are `crm24-resumed-go-{build,vet,lint}-20261126.log`.
- The outer sequential driver reached its one-hour execution limit during frontend tests. Its exit `143` is an interrupted pipeline, not a frontend pass. The partial verbose log records 758 passing cases across 60 files without a final summary; file hooks and suite completion must be verified before any result is retained. Frontend test/lint/build and publication remain pending.

## Complete frontend verification and newer master integration

- All ten native frontend batches subsequently exited `0` with JSON success, zero failed suites, zero failed tests and zero pending tests. The independent verifier covers exactly 145 files and 1,329 passing cases, without using the interrupted log. Frontend lint and production build also exited `0`.
- Native JSON identifies titles using `ancestorTitles` plus `title`; the collector joins those with ` > `. The external verifier corrected its own name-format comparison. One existing `merchantConnectionWizard.test.js` case embeds `new Date(Date.now() + 3600000)` in its generated title; only that uniquely identified future-date title is normalized for identity matching. Raw titles and source provenance are retained, and test assertions remain unchanged. Evidence: `crm24-frontend-verified-20261126.json` and the ten native batch reports.
- Both official remotes then advanced to `838ceb4681013e219bb8e70c17b9af0e5d5ea463`. The isolated release clone fast-forwarded safely; all 85 owned file hashes and the exact index were preserved, and the recovery stash was retained. Evidence: `crm24-master-integration-20261126.json`.
- The preceding Go/frontend passes describe the pre-integration snapshot. A complete integrated Go/frontend/static gate run is now in progress with separate source fingerprints and logs (`crm24-integrated-final-gates-20261126.json`). Commit, dual push and production deployment remain pending until the integrated checks complete.

## Integrated Go evidence and interrupted-driver recovery — 2026-11-26

- On integrated master `838ceb4681013e219bb8e70c17b9af0e5d5ea463` plus the 85 owned changes, `go test -json ./...` completed from `12:27:34` to `12:56:52` with exit `0`. All 195 packages are accounted for: 158 passed and 37 reported no test files; none failed or is missing. The native log records 21,009 passing test events and 122 skipped test events, so this is not a claim that every available test executed.
- The separate mandatory-case verifier confirms all 19 required CRM authorization and release-blocker regressions and their subcases passed without skips. Evidence: `crm24-integrated-go-mandatory-verified-20261126.json` and the retained `crm24-integrated-go-test-20261126.log` in the approved task temporary directory.
- The harness process daemon stopped after the completed Go result had been saved. Recovery verified unchanged source fingerprints and no remaining owned test process, preserved the prior state, and resumed only unfinished gates. Integrated build passed at `13:01:01`; the remaining integrated checks and production workflow still require completion. The completed Go gate is retained without rerunning it merely because the harness restarted.

## Completed prior snapshot and subsequent master reconciliation — 2026-11-26

- The complete 12-gate pipeline for the `838ceb4681013e219bb8e70c17b9af0e5d5ea463` snapshot finished at `13:17:19`, with every command exiting `0`. Independent verification at `13:17:36` confirmed the complete Go package set, all 19 mandatory regressions and subcases without skips, and native frontend success across 145 files and 1,327 cases, with zero failed or pending cases. Source fingerprints were unchanged. Evidence: `crm24-integrated-verified-20261126.json`; the earlier 1,329-case batch result belongs to a different snapshot.
- Unavailable coverage, local API and sibling-repository audit inputs remain explicit SKIPs. Graph refresh remains unavailable because the Python environment lacks `graphify`.
- Both official remotes then matched newer master `f0309fea7ca2c5bcdaef5bdfb5fc14456b8002cd`. The complete owned patch, index tree and file hashes were saved before fast-forwarding the isolated clone; stash `3808fcba882567c76ac0bd78a0999db90043c3c8` is retained. Six overlapping paths were reconciled with upstream changes, preserving tenant pinning, operational-error propagation and every existing assertion. Twenty of the 85 owned paths were already upstream, leaving 65 explicitly staged owned paths.
- A separate 13-gate run verifies the actual newer source, including its collected set of 147 frontend files. Evidence uses the `f0309fea7` suffix; prior passing results are not relabelled as coverage of this source. Commit, dual push and CRM-24 deployment remain pending this result.
- Independent read-only production checks confirmed the concurrent `13:17:57 +08:00` release is `f0309fea7`: host, container and running-process hashes, image and start time match its state, and health is normal. Only the isolated clone's ignored deployment baseline was reconciled. Evidence: `crm24-deploy-baseline-reconciled-f030-20261126.json`.

## Controller fixture integration repair — 2026-11-26

- Compile-only RED at `13:34:40`: `go test ./internal/controller -run '^$'` exited `1`, identifying two upload fixtures still referencing the removed `ctrlSageXChannelRepo` name after conflict resolution retained upstream `ctrlBootstrapChannelRepo`. Evidence: `crm24-f030-controller-fixture-compile-red-20261126.{json,log}`.
- Before changing the source, stopped only the owned verification driver and its descendants at `13:35:00`, retaining its incomplete state and logs. That first `f0309fea7` run is aborted evidence, not a passing suite. Evidence: `crm24-f030-driver-aborted-for-fixture-repair-20261126.json`.
- Reused the existing tenant/ID-validating `ctrlBootstrapChannelRepo` in the two fixture constructors. No assertion, production behavior, or test threshold changed. Focused GREEN: `go test ./internal/controller -run TestSageX -count=1`, exit `0`, completed `13:37:46`; package time `0.798s`. Evidence: `crm24-f030-controller-fixture-green-20261126.{json,log}`.
- Started one separate full 13-gate run for the corrected source with evidence tag `f030-fixturefix`. This is the current verification run; its terminal result and independent source/package/frontend checks are required before commit and release.

## Completed RED, contract repair and current full run — 2026-11-26

- The f030-fixturefix full Go gate completed with all 195 package terminals accounted for and seven failed tests across controller/model/repository/service. The native RED result is retained; diagnosis is `crm24-f030-fixturefix-go-failure-analysis-20261126.json` in the approved task temporary directory.
- Exactly four additional test files were repaired for the new admin-only channels container, current KB embedding audit contract, and complete lead-mining detection evidence. Existing role/leaf/path fingerprints and business assertions remain; four incomplete-evidence rejection subcases additionally prove no customer/clue side effects.
- Focused GREEN at 14:18:49: four packages, 70 passing test events, zero failures/skips, exit 0. Evidence: `crm24-f030-seven-blockers-focused-green-v2-20261126.{json,log}`. The preceding test-only compile error and its correction remain separately preserved.
- `crm24-f030-seven-blockers-repair-scope-20261126.json` confirms exactly those four test-only changes since completed RED and 69 owned staged paths. Index tree before report updates was `bee405676bb993199a809400fa301fe76303a860`.
- One fresh complete 13-gate run started at 14:19:34 with tag f030-contractfix. Recovery confirms 147 frontend files collected and controller/model/repository passes; no terminal result yet. Independent verification requires all 195 Go packages, all 39 mandatory top-level regressions and subcases without skips, exact frontend collection coverage and unchanged source fingerprints. No publication or task closure is claimed.

## Completed frontend RED and focused repair — 2026-11-26

- f030-contractfix completed RED at 14:44:34. Independent evidence confirms all 195 Go package terminals (158 pass, 37 no-test-file skip), all 39 mandatory regressions and subcases passed, and unchanged source/HEAD. The 122 other Go test skips remain explicit; they do not include mandatory cases. Build, vet and lint passed.
- Native frontend coverage was exactly 147 files and 1,335 cases: 1,332 passed, three failed, zero pending. Two clue-list failures were 15-second timeouts; one menu authority assertion failed because an upstream merge restored deliberately removed regrouping. Subsequent pipeline gates did not execute. Evidence is the immutable f030-contractfix final-gates, native frontend and independent verification JSON.
- Before production repair, crm24-f030-navigation-red-clue-diagnostic-20261126.{json,log} recorded 25/27 passing at 14:47:42. The existing authority assertion and new combined regression failed; all nine unchanged clue-list cases passed. No timeout or business assertion changed.
- Removed only the obsolete regrouping block, retaining channel aggregation. crm24-f030-navigation-focused-green-20261126.{json,log} and native JSON record 36/36 passing in four files at 14:48:50, including navigation, original content regression, clue filters and onboarding.
- The new source snapshot requires its own complete 13-gate f030-navfix run and independent source/package/39-regression/frontend checks. Scope is 71 owned paths and includes SPA publication through documented chain D after committed-source verification. CRM24 stays open.

## Complete GREEN and independent verification — 2026-11-26

- f030-navfix finished at 14:58:44 with all 13 gate exits zero: frontend collection, complete Go tests, build, vet, lint, complete frontend tests, frontend lint/build, audit, format, indexed Markdown links and working/staged whitespace.
- Independent verifier at 14:58:46 passed every check. Go: 195 package terminals (158 pass, 37 no-test-file skips), 21,064 passing test events and 122 optional skips; all 39 mandatory regressions and their subcases passed without skips. Frontend: exact 147 collected files, 1,336 total/passed, zero failed/pending, native success true. Tested HEAD/source unchanged.
- Immutable evidence: crm24-integrated-final-gates-f030-navfix-20261126.json, crm24-integrated-verified-f030-navfix-20261126.json and crm24-integrated-frontend-native-f030-navfix-20261126.json. Audit SKIPs are not verified; production acceptance remains separate.
- Graph proof and credential preflight are independently recorded in crm24-f030-navfix-graph-refresh-20261126.json and crm24-f030-navfix-prepublication-credentials-20261126.json. Graph question ranking is approximate (64 sampled sources, seed 0); graph/communities include all tracked supported source. Both credential gates passed with zero private occurrences.
- Publication must match this source byte-for-byte, synchronize both official masters, run API tests enabled and SPA chain D, then verify runtime/artifacts and nine authenticated read-only GETs. CRM24 remains open until that evidence exists.

## 2026-11-26 CRM24 正式发布与只读验收完成

- 正式源码：`b60564342489ba89540cdac64607fa79cdfd95f2`；本地 master 已提交并同步双远端，发布与验收均对应同一源码。
- API 链 A：默认测试开启，正式脚本退出码 0；开始 `2026-11-26T15:06:25`，完成 `2026-11-26T15:30:25`；独立产物/容器/运行进程/健康检查等 13 项全部通过。
- API 二进制 SHA-256：`6c27c7507615694029674037c6fce31aefa57a343ad2986a986772aaaf43bfb1`；镜像 `sha256:075feead39feb41ca11ab7f562c7cf1e64a95ad1fd065e9d1978c917933c4a3f`；容器启动 `2026-11-26T07:30:14.371894627Z`。
- API 回滚点：`mtk-user-server:rollback-20261126-150626`；二进制备份 `/www/wwwroot/hivemtk/user-server/user-server.bak-20261126-150626`；默认口令探针拒绝默认凭据。
- SPA 链 D：构建 `b60564342-1795678239997`；主资源 `assets/index-BFR_IG4d.js`；备份 `/www/wwwroot/hivemtk/user-web.snapshot-20261126-153047.tgz`；704 个宿主产物逐个摘要一致，公网入口/版本/worker/主资源摘要一致，6 项检查全部通过。
- 生产只读验收：`2026-11-26T07:30:52.267Z`，9/9 项 HTTP 200 且业务 code=0；覆盖当前用户、客户列表/详情/360、联系人、供应商、服务商、合并审计及跨行业列表。
- 验证边界：生产使用现有管理员会话做只读冒烟；受限账号授权矩阵以本地 PostgreSQL 必跑回归为证。未执行会写操作审计的导出 GET、dedupe POST 或客户写接口。
- 完整回归证据继续有效：13 道综合门、195 个 Go 包终态、39 项必跑回归及子用例、147 个前端文件/1,336 用例；122 项可选 Go SKIP 与审计 SKIP 保留未验证说明。源码图完整，问题排序的 64 节点抽样不等于完整中心性计算。
- 原始证据：`crm24-official-api-deploy-20261126.json`、`crm24-production-deployment-verification-20261126.json`、`crm24-production-frontend-deployment-verification-20261126.json`、`crm24-production-readonly-acceptance-20261126.json`。
