# 反例攻击任务（lane 6：审核门与驱动器自身有没有牙）

你是独立的攻击者。你审的不是业务代码，是**用来收口的几份脚本**。你的唯一问题：

> **哪一处最小改动（注码）会让这些脚本对外承诺的判定能力在语义上被破坏，而它自己的自检仍全绿？**

## 必读范围

- 协议文档：`docs/superpowers/specs/2026-09-22-second-review-protocol-design.md` 的 §3（九条判据）
  与 §8（验收判据）。**只读这一份的 §3 与 §8**，别读 §7（证据）。
- 被审对象（`scripts/` 下）：`check-review-closeout.py`、`build-review-ledger.py`、`merge-gate.py`、
  `ledger_r22.py`、`run-gate-in-clone.py`、`check-architecture.sh` + `arch-l4-getdb.baseline`、
  `reverse-test-l4-baseline.sh`、`check-async-global-read.py`、`check-seam-guard.py`（存在哪些看 `ls`）。

## 攻击方向（类别，不是答案）

每条判据都要问："摘掉它，有没有**反向测试格**会红？"具体形态：
判据分支换成 `pass` / 期望值写死成实抽值 / `WANT` 表关键字放宽到任何红都算对 /
`mtime ≥ first_seen` 改成 `>` 或去掉 / `/tmp/**` 排除规则的写法漏洞 / `DIAG` 豁免表把真门降成诊断步 /
`check_diag_names` 的名单核对 / `preflight` 里"命令点名的文件必须存在"这一格 /
流式 `Transcript` 是否真在跑之前就落盘（否则"引用本轮日志"永远凑不齐）/
`--only` 过滤成 0 步是否退 0 / 单向门（只查"超出"不查"收窄"）。

## 明令禁止

- **不许读** `docs/superpowers/specs/ledger/*.jsonl`（台账条目）与其下任何日志。
- **不许编辑任何文件**；**不许在仓内留下任何产物**（要跑脚本就 `cd` 到 `/tmp` 下的临时副本里跑，
  用完删掉）。允许直接 `python3 <脚本> --selftest`、`--help`、`bash -n`、grep、读源码。
- **不许跑全量 `go test`**（共享树）。

## 输出（≤600 字，中文）

四字段一条：`被破坏的判据/承诺（编号）` / `注码 <脚本>:<行>「把 X 改成 Y」`（语义注码，
禁摘整块致语法错——脚本坏了退非 0 会被误当成"判红了"）/
`预言哪一格自检/反向格该红（写格名或 `WANT` 键）` / `凭什么它不红`。
找不到红格就说找不到。最后一行：`未验证：候选均未实际注码运行。`
