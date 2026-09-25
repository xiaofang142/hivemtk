<!-- 恢复件：本文件是 B 相 lane 5 子代理的**原样回报正文**，由主会话于 2026-09-23 04:2x 从子代理逐字记录 `agent-ageneral-purpose-111927abc88c182e.jsonl` 的最后一条 assistant 文本里抽出落盘（派发时间 02:2x，报告当时只留在会话里、没进磁盘）。它是**二手候选**：每条都带「未验证：候选均未实际注码运行」的自陈，逐条的重验结果在 `logs/c-phase/` 与主 spec §八/§7.29。-->

四条承诺各找到一处可存活的最小语义注码（未跑测试，仅按源码判腿）。

**承诺 3（重推上界 / 落终态）**
- 注码 `user-server/internal/repository/message_hub_inbox_outbound.go:71`「把 `exhaustOutbound` 实参末位 `cutoff` 改成 `time.Now()`」——等价于把 :70 的终态化谓词从「inflight 且认领已超时」放宽成「凡 inflight」，占位符数不变、可编译。
- 该红的腿：`TestPushCapSweepTouchesOnlyOwedOutbound`、`TestFetchOutboundUndeliveredExcludesAndEscalatesExhausted` 里该有一格「界内 + 未超时 ⇒ 行仍是 inflight，不许判 failed」。
- 凭什么不红：五条 b20d 腿到界前从不造未超时样本，到界后都先把 `claimed_at` 拨旧 1h；真正有牙的超时腿 `TestClaimOutboundForPush_FreshInflightIsExclusive`（`message_hub_outbound_push_b11_test.go:58`）与 `TestFetchOutboundUndelivered_StatusNotCursor` 的 holding（`:140`）其行 `push_attempts=0`，被 `push_attempts >= ?` 先挡在收口之外。后果：正被桥端发送的行就地 failed，随后 ack 的 `status IN ('pending','inflight')` 打空——客户收到了、库里说失败。

**承诺 4（SentCache 24h TTL / 最后命中序）**
- 注码 `user-web/bridge/src/core/downlink.js:63`「删掉 `add()` 里的 `this.mem.delete(id)`，只留 `set`」——命中已有键退回原位改值。
- 该红的腿：`downlink-b20d-sentcache-ttl.test.js` 的 B 格（记录以新时间戳续期）、C 格（插入序 vs 时间序错位）。
- 不红：B 的 25h 记录在 `load()`→`evict`（:72-75）就删了，add 时键不存在；G 格只断 `sendOutbound` 次数；C 的错位由 :47 的 sort 摆平，轮询里只新增一个键 ⇒「先删后设」整条分支无用例走过。后果：`evict` 的「头部即最旧」前缀不变式破了（头部一 fresh 就 `break`，界外条目永不清），淘汰回到 row 4 明令否掉的插入序。

**承诺 7（拒绝心跳，靠固定 30s 认领自愈）**
- 注码 `user-server/internal/service/inbox_ingress_outbound.go:164`「`30 * time.Second` → `5 * time.Second`」。
- 该红的腿：一条「可见性超时 ≥ 单条下行发送预算」的下界腿（row 4 的 E 格正是这么钉 TTL 的）。
- 不红：全仓两处引用写作 `== service.InboxOutboundClaimTimeout`（`sse_outbound_claim_b11_test.go:81`、`:280`）——自我参照，改值随动；唯一数值面 `inbox_ingress_boundary_test.go:364` 把 `claimed_at` 拨旧 60s，只夹上界（放大才红，压小无感）。后果：行在桥端仍在发（`sendOutboundTimeoutMs:20000`）时就被两条路径各拿一次 → 双投，且 A3「约 10 分钟」口径失真。

**承诺 8（结论位永不参与 ack / markSent / 重推）**
- 注码 `user-web/bridge/src/core/channel-adapter.js:1268`「把 `this.rateLimiter.markSent(...)` 包进 `if (sendVerified)`」。
- 该红的腿：`adapter-b24-send-verify.test.js`「反向半边：平台静默吞…照常 ack 语义不变」应连带断 `cs.lastHash/lastSentAt` 仍被写。
- 不红：该用例只 spy `rawSendText` 与回声帧；`rate-limiter.test.js:14-50` 的 dedup 腿直调 `rl.markSent`、不经 `sendOutbound`；`batch3-hygiene.test.js:104-118` 的静态锁只比顺序。对照之下 ack 那侧反倒有腿（`downlink.js:411` 改成「未见就不 ack」会立刻打红 b20d B/C/F）。后果：回查未见 ⇒ 同文本去重位不记 ⇒ 下一句同文案直过——结论位成了第二道闸门。

无存活注码的面：TTL 读侧 `has()`、三条认领路径的 `push_attempts` 界、回收不重置计数、回查只读红线②（`_sentKeys`/`seenNodes` 快照），均各有具名腿。

未验证：候选均未实际注码运行。
