# 已排队（未开工）· 4-D：`_dedupKey` 键形零断言，注释却把它写成硬承诺

**为什么排队**：这条要动 `user-web/bridge/src/core/`，另一条泳道此刻正在改
`user-web/bridge/src/core/channel-adapter.js`（把 `markSent` 从 `sendVerified` 闸门里摘出来）。
两个改动者同时写同一文件＝共享树上互相抹改动，必须串行。**开工前置：那条泳道回报完成。**

## 靶子

- 位置：`user-web/bridge/src/core/channel-adapter.js:157` 注释「键形一变就等于全部重报一遍」+ `:162`
  `occurrence > 0 ? \`${base}#${occurrence}\` : base`
- 上游（只读审查线）判断：`test/adapter-r23-occurrence-identity.test.js` 四条 it 实跑 4 passed，
  但断言全部打在 `_canonicalMsgId`（`:191-197`）产出的线上 `event_id`；
  `_dedupKey` 只喂 `_hasSent/_markSent`（`:199-213`）与 `_bumpOccurrence`，**其字符串形状全仓零断言**。
  ⇒ 把 `> 0` 改成 `>= 0`（首条键形变 `base#0`）没有任何腿会红，而注释把它写成硬承诺。

## 开工顺序（与 lane 4 同：先证实再补）

1. 注码 `> 0` → `>= 0`，`cd user-web/bridge && npx vitest run test/adapter-r23-occurrence-identity.test.js`
   看是否真的 4 passed 不红（先 `cp` 备份 + md5，禁 git restore）。
2. 注码不红 ⇒ 缺口成立：补腿断"首条 `_dedupKey` 必须等于裸 base、第二条起才带 `#n`"，
   并且**同时要一条反向腿**：键形带上 `#0` 时旧记录不再命中（即"全部重报一遍"真的会发生），
   不然新腿只是把当前实现抄一遍。
3. 补完后去掉注码，验新腿在好码上绿、在注码上红（两向都测过才算有牙）。

## 顺带要防的形状

`user-web/bridge/src/core/downlink.js` 的 `SentCache.add()`（`:60-67`，`this.mem.delete(id)` 后再 set）
与 `evict()`（`:70-79`，头部前缀 break）是另一条泳道的靶子；本条只碰 `_dedupKey`，
别把两件事塞进一次改动——同一文件两个改动者正是要排队的原因。
