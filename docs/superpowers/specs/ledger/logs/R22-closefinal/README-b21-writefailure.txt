# b21 写失败格（TestWriteFailureIsNotNeverOnWire）竞态取证

统一命令（工作目录 `user-server/`，`-run` 只过滤这一条用例，**不是门禁**）：

```
go test ./internal/browser_automation/service/ -run 'TestWriteFailureIsNotNeverOnWire$' -count=N [-v] [-cpu=1]
```

`夹具红` 列 = `grep -c '夹具必须命中'`，即落进错误分支 `host_offline_b21_test.go` 那句
`t.Fatalf("夹具必须命中 writeJSON 失败分支…")` 的次数（真红因是 Request 先看到「取不到连接」）。

| 文件 | 树状态 | 参数 | 读数 |
|---|---|---|---|
| `b21-writefailure-defaultcpu-count1-run1.log` | 修复前 | `-count=1`（无 `-v`） | `ok 0.351s`，单跑抽不中 |
| `b21-writefailure-defaultcpu-count500-run1.log` | 修复前 | `-count=500 -v` | 485 PASS + 15 FAIL ⇒ 夹具红 15 |
| `b21-writefailure-defaultcpu-count1000-run1-before.log` | 修复前 | `-count=1000 -v` | 853 PASS + 36 FAIL ⇒ 夹具红 36 |
| `b21-writefailure-defaultcpu-count1000-run2-swapconn.log` | 中间一版（Register 后换 HostConn 占同一个 key） | `-count=1000 -v` | 88 PASS 后 `panic: concurrent write to websocket connection` 带走整个二进制，余下未跑 ⇒ 该版作废 |
| `b21-writefailure-cpu1-count500-run1-swapconn.log` | 同一作废版 | `-cpu=1 -count=500 -v` | 500 PASS，rc=0（单核调度抽不中，不能据此放过并发写） |
| `b21-writefailure-defaultcpu-count2000-run1-finalfix.log` | 终版（不 Register，手造无 goroutine 的连接登记进表） | `-count=2000 -v` | 2000 PASS / 0 FAIL / 0 panic，rc=0 |
| `b21-writefailure-mut-wrappedneveronwire-run1.log` | 终版 + 变异（`host_registry.go` 写失败分支改成并 `ErrCommandNeverOnWire`） | 四条 b21 用例 | 本条 FAIL（183、186 两句各红一次），另三条仍 PASS ⇒ 判据有牙且红因特异 |
| `b21-writefailure-defaultcpu-count50-run1-postrestore.log` | 变异还原后（md5 与备份一致 `a5d732da…`） | `-count=50`（无 `-v`） | `ok 0.305s` |

两个本轮踩到/清掉的坑（都写进记忆，不只写在这里）：

1. **`-cpu=1 -count=1` 那次「确定性复现」是假的**：zsh 不对未加引号的 `$spec` 拆词，整串被当成
   一个参数传给 `-cpu` ⇒ `testing: invalid value "1 -count=200" for -test.cpu`、rc=1、却没有
   一行 `--- FAIL`。这份产物已从取证目录删掉（它不是一份读数）。真实复现率见上表：默认调度下
   约 3%（15/500、36/1000）。
2. 抽中之前的第一版「修复」（同一个 socket 上再造一个 `HostConn` 占住 key）把竞态换成了
   gorilla 的并发写 panic：`Register` 起的 `probeServable` 与测试侧各持自己那份 `writeMu`，
   锁保护不了同一个 `*websocket.Conn`（`host_registry.go:437` 的栈在
   `…-count1000-run2-swapconn.log` 里）。终版因此不注册，改为手造一条没有 goroutine 的连接。
