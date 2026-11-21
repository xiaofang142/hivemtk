# Bug Assessment: SYS-01 后续静态门阻塞

- **Slug**: sys01-local-gates-20261120-2
- **Created**: 2026-11-20
- **Source**: 当前完整 audit/lint 的实际输出
- **Verdict**: valid
- **Severity**: low

## Reproduction and Root Cause

第一轮门修复后，完整 `make audit` 退出 2，运行到 `check-bare-go.py` 后因 `kb_crawler.go` 登记 2 / 实际 0 而报 STALE。同步爬取已经移除了旧协程，正确处理是删除失效条目，进一步降低基线；不添加豁免、不提高数值。

`make lint` 退出 2：`knowledge.go:362 persistChunkEmbeddings is unused`。全 user-server 的 Go 源码搜索只有该方法声明。FA-13 已改为有归属栅栏的持久化入口，其他调用也使用保留 source 的入口；这层旧的默认 TEI 包装没有调用者。删除该三行私有死方法，不改变任何运行路径。

日志：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/sys01-closure/{audit,lint}.log`。

## Proposed Remediation

**Files likely to change**:

- `scripts/bare-go.baseline`：仅删除 kb_crawler.go 的 2 个已清零站点。
- `user-server/internal/aiagent/knowledge/service/knowledge.go`：仅删除无人调用的 persistChunkEmbeddings 包装。

**Tests/checks**:

- `python3 scripts/check-bare-go.py` 先复核实际计数，再修复并运行脚本已有反向控制测试。
- 再跑完整 audit 与 lint；保留完整 Go 回归，以及 FA-12/FA-13 行为测试。
- 不为死代码移除编写镜像单测；这两项已有真实静态 RED，不声称存在新的运行时行为缺陷。

## Risks and Scope

没有行为/接口/数据库变化。原始辅助 `check_naked_goroutine.sh` 对多域大量既有站点报错，与支持棘轮的当前 audit 不同；不跨越单任务范围重写其他域或弱化此脚本，保留该独立检查的真实失败证据。
