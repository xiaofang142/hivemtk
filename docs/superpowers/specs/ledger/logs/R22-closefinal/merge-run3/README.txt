# 第 3 趟全量门禁（末次跑之前那一趟）：33/34，红在本泳道的 b21 夹具竞态

被第 4 趟原地覆盖前先另存一份（`merge/` 下的同名文件每次全量跑都会刷掉）。

判定行（`merge-gate-full-run3.log` 末行）：

```
===== 合并门禁：33/34 门步绿，红在：go-test-browser-automation；诊断步红 1 道不门控：ci-step-coverage =====
```

这一趟的红**不是并行 lane 的在飞窗口，是本泳道自己的**：`go-test-browser-automation` 243.1s，红因
`host_offline_b21_test.go` 里 `夹具必须命中 writeJSON 失败分支` 那句 `t.Fatalf` —— 夹具竞态，
抽中率实测 15/500、36/1000；修复与复验读数在同目录 `README-b21-writefailure.txt` 与主文档 §五.6。

本目录另存了四份步骤日志（`go-test-browser-automation`、`go-test-host-packages`、
`closeout-r22`、`closeout-r22-protocol`），其余步骤与第 4 趟同读数、无需另存。
