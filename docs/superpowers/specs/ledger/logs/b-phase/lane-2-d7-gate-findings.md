<!-- 恢复件：本文件是 B 相 lane 2 子代理的**原样回报正文**，由主会话于 2026-09-23 04:2x 从子代理逐字记录 `agent-ageneral-purpose-83ba17da1e615596.jsonl` 的最后一条 assistant 文本里抽出落盘（派发时间 02:2x，报告当时只留在会话里、没进磁盘）。它是**二手候选**：每条都带「未验证：候选均未实际注码运行」的自陈，逐条的重验结果在 `logs/c-phase/` 与主 spec §八/§7.29。-->

**先记一处口径差**：brief 写的 `user-server/internal/service/` 不存在，实际落在 `user-server/internal/browser_automation/service|controller/`。腿的判定用 `go test -list`（两包均编译通过，184 条）+ 读测试源码，未跑任何包。

**承诺 5（A5 审批绑载荷）**
注码 `user-server/internal/browser_automation/service/executor.go:448`「把 `const d7PreviewRunes = 200` 改成 `= 0`」——489 行的 preview 于是恒为一个 `…`。
预言该红：`TestPendingGateExposesAwaitingPayload`、`TestConfirmGateReadIsOwnershipScoped`。
凭什么不红：这两条都在用例里手写 `&confirmGate{preview:…}`（d7_gate_b20_test.go:123、:346）塞进注册表，从不走 executor.go:489 这个唯一生产构造点；两条真挂起的腿（`TestWSE2E_D7GateHoldsSendUntilConfirmed`、`TestUnknownEffectStepHoldsConfirmGate`）只断 `PayloadHash`；正文零泄漏那条反而更稳。结果：批准者只剩 8 位十六进制指纹，行 446 注释自陈的「让人看见自己批的是什么＝A5 的前提」被掏空，回到盲签。

**承诺 14（A9 审批进审计链）**
注码 `…/service/executor.go:507` 与 `:509`「把 judge 帧的 `stepID` 改成 `0`、`"step_index": stepIndex` 改成 `"step_index": 0`」。
预言该红：`TestConfirmGateFramesBindPayloadAndOmitBody`。
凭什么不红：该腿把步骤绑定只断在 d7_wait 帧上（:171-172），对 judge 帧只断 direction / ok / payload_hash / decision / mismatch_attempts / seq。结果：d7_confirm 不再回答「放行落在哪一格提交点」，同文本挂在不同 step_index（批7 F-N4 形态）时两帧无法区分。
补充：三条出路的 decision、`mismatch_attempts`、seq 单调、正文不落帧，各自有腿，摘不动。

**承诺 15（A10 挂起态可跨进程查证）——两条**
注码 A `…/service/session.go:142`「把 `l.Seq > latest.Seq` 改成 `l.Seq < latest.Seq`」（取最旧帧而非最新帧）。
预言该红：`TestConfirmStatusDistinguishesGateElsewhere`、`TestB20ConfirmGateElsewhereFromAuditFrame`。
凭什么不红：两处替身/真库种子每个 session 各只有一帧 d7_wait（d7_gate_b20_test.go:297、:308；controller 的 `seedWaitFrame` Seq 恒 1），单元素集合上取 max 与取 min 同解。结果：多写步会话里「库里存在未到期 d7_wait」被最旧那条已过期帧顶掉，`gate_on_another_instance` 永远查不出来，正是本行要消灭的「折成一句没有待确认」。
注码 B `…/service/executor.go:498`「把 `gate.expiresAt.Format(RFC3339Nano)` 改成 `time.Now().Add(24*time.Hour).Format(RFC3339Nano)`」。
预言该红：同上两条 + `TestConfirmGateFramesBindPayloadAndOmitBody`。
凭什么不红：写入侧对 expires_at 只断「非空字符串」（:174），消费侧读的都是测试自己种下的帧，生产值与真值从未在同一条帧上对上。结果：`expires_at` 与内存真值 AND 的口径失效（写下的期望 ≠ 默认 600s 预算），本地已 timeout 收口的会话仍被任意实例回 409「闸门在另一个进程」24 小时——session.go:126-128 注释自陈要防的「旧帧把会话永久钉死」被反向兑现。

**总判断**：三条承诺的核心谓词（哈希比对、先摘后关、三态归因、空载荷 panic、状态门、归属校验、expires_at 与真值 AND）都有腿；能注进去的是「这条数据由谁生产」那一半——预览值、judge 帧步骤绑定、跨帧择新、写入的 expires_at，全部是消费侧有断言、生产侧无腿。

未验证：所有候选均未在真树上注码跑过。
