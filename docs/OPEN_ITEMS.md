# 当前未完成事项（手工维护，唯一台账）

> 维护口径：只放**仍需要动作**的条目。已交付的历史规划已从工作树清理
> （`改进清单.md`、`未完成任务.md` 见 git 历史），不要再往回加流水账。
> 更新方式：完成后直接删行，不要留着打 ✅。

## 1. user 桶死路由（执行选项①：保守保留 + 登记边界）

- **状态**：**已按选项①收口**（2026-10-26）。不删除，边界登记在本条。原条目里的
  「419 条 / unverified 127 条」是**旧读数，已被现测推翻**（见下），不要照那个数复算。
- **现测（2026-10-26，`scripts/route-consumer-triage.py`，HEAD `11185fa4`+B2）**：
  路由表 1898 行 → 命中 client **1126** ｜ 只命中 ops/self-page **276** ｜
  只有弱证据（祖生前缀命中）**171** ｜ **三面全不命中 325**。
  名册四档：`bucket-client.tsv` / `bucket-ops.tsv` / `bucket-weak.tsv` / `bucket-none.tsv`。
  复算：
  ```bash
  cd hivemtk && python3 scripts/route-consumer-triage.py --out /tmp/rt
  wc -l /tmp/rt/bucket-*.tsv /tmp/rt/live_routes.tsv
  ```
- **为什么"三面全不命中"不能直接删**（工具自己也在收尾时这么说，这条是判据不是免责）：
  1. **地址栏直开口**：`GET /api/export/xxx` 这类由运营手敲/书签打开的口，前端源码里没有调用点；
  2. **渠道与 ESP 回调**：`/api/wecom/callback`、`/api/feishu/event` 之类是被**外部平台**调的，
     消费方不在本仓；
  3. **版本不可知的老前端构建**：线上可能还跑着旧 bundle，删了口就是"老页面突然 404"。
  三类静态扫不到，而**移动端（uniapp）在外部仓**，仓内无法证明它是否消费某个口 ——
  这就是原条目「禁止凭静态分析直接删」的理由，仍然成立。
- **真要推进选项②需要的外部证据**（任一条即可开工，不必等全部）：
  ① 移动端仓的路由调用清单（`grep -r "/api/" uniapp/src` 的产出）；
  ② 生产网关 N 天访问日志按 path 聚合（证明 0 请求）；
  ③ 产品确认这批口没有对外承诺（开放 API / 文档站里出现过的一律留）。
- **已净删的部分不在此列**：I27 的 admin 桶 156 条是另一轮判定（有"菜单/前端/路由三方对账"
  证据支撑），口径不可套用到 user 桶。

## 2. `internal/service` 单包预算（收口后只剩架构动作）

- **已收口的部分**（2026-10-26）：全量 `-p 1` 实测 **648.6s**，Go 默认单包预算 600s，
  所以裸敲 `go test ./internal/service` 会 `panic: test timed out`，与代码无关。CI 侧
  早在用 `-timeout 2400s`（`user-server-ci.yml:738/820`），`make test-go` 用 900s，
  本轮补了 `make test-service`（`-timeout 30m`）供单包复跑。三处入口现在都不再被默认值掐。
- **仍需要动作的**：按子域拆包，把这一包的墙钟降回分钟级。属架构动作，拆之前要先把
  `internal/pkg/testutil/testdb.go` 的槽位认领改成按子域隔离，否则拆了还是串行。
- **已撤销的一条**：旧记录说 `check-battery-identity.py` 的豁免依赖被 gitignore 的轮次目录、
  干净克隆会红。2026-10-26 在干净 worktree（`git worktree add --detach /tmp/hm-clean-1026 11185fa4`）
  复算，`--selftest` 与 `--quiet-ok` 都是 **rc=0**（`失败 0 格` / `0 项不合格`），
  轮次目录 `docs/superpowers/specs/ledger/logs/` 一直在树里。
