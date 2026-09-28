<!-- 恢复件：C 相复验子代理 `3f02effdb17` 的回报正文（取该会话最长的一条 assistant 文本）。活动窗口 09-22T18:53→09-22T19:00Z（本地 +8）；装它的任务书＝queued-4d-dedupkey-shape.md。正文一字未改，由主会话 09-23 05:0x 从子代理逐字记录抽出落盘。 -->

## 结论：缺口成立，已补三条带牙的腿（未提交 git）

### 0. 开工门禁核对
- `user-web/bridge/src/core/channel-adapter.js` = `6af6de63da9ac20a83e6d8ea8ebcbab1` ✓
- `downlink.js` **首次读到 `08561459…`、再读 `b3021bbf…`（mtime 02:54，就在几秒前）⇒ 那条泳道当时正在做 cp 还原/注码循环，没落定**。我没在残迹上干活，等到它落定在 `712a0b00292f4671489a98ccb4c59fbc` ✓（02:56:05 记档），整个作业期间每次跑测前后复核，始终 = golden（末次 03:00:05）。我未使用任何 `git checkout/restore/stash`，只用了 `cp`（备份 `/tmp/lane4d-bak/`）与 Edit。

### 1. 注码实跑（只翻 `:162` 一处，`:194` 的 `_canonicalMsgId` 未碰）
注码后 md5 `396b6aeb9189968a451fffed5c363dfb`；`diff`（反向替换后比对备份）＝ `NO_OTHER_CHANGE_VS_BACKUP`，即与 golden 只差 162 行一处。

| 跑法 | 好码基线 | 注码下 |
|---|---|---|
| `npx vitest run test/adapter-r23-occurrence-identity.test.js` | `Tests 4 passed (4)` rc=0 | `Tests 4 passed (4)` rc=0 |
| `npx vitest run`（整包） | `Test Files 58 passed (58)` / `Tests 766 passed | 7 skipped (773)` / rc=0 | `Test Files 58 passed (58)` / `Tests 766 passed | 7 skipped (773)` / rc=0 |

**读数逐字一致 ⇒ 上游判断成立**：`:162` 键形全仓零断言，注码后没有任何腿红。（复核过那四条腿确实全打在 `_canonicalMsgId` 产出的 `event_id`/`message_id`；`test/adapter-b24-send-verify.test.js:212-217` 只比较 `_sentKeys.keys()` 的前后快照，形状无关；`adapter-b24b…:102` 断的是计数，两侧都走同一个函数，同样形状无关。）

### 2. 新腿（一个文件，未动别人的测试文件，未动生产码）
`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/user-web/bridge/test/adapter-dedupkey-shape.test.js`
- `:63` ① 正向形状：首条裸 base、第二条起才 `#<n>`（含直接问 `_dedupKey` 与 `_sentKeys` 账本同形）
- `:82` ② 落盘形状：`stop()` flush 进 localStorage 后首条记录仍是裸 base（注释里「落 localStorage」那半句）
- `:97` ③ **反向半边（有牙的那条）**：按契约形状独立写入「老版本留下的裸 base 账」，新实例首扫必须命中并吞掉；键形一变就不同名 ⇒ 「全部重报一遍」真发生

期望键用 `adapter._hash(\`${CID}|${SENDER_ID}|${TEXT}\`)` 独立复算，不让被测函数给自己当标准。

### 3. 两向读数
- 注码下（好码 3 passed → 注码）rc=1，`Tests 3 failed (3)`，逐字：
  - `AssertionError: expected '2bvjuk0tju8#0' to be '2bvjuk0tju8'`（①）
  - `AssertionError: expected [ '2bvjuk0tju8#0', '2bvjuk0tju8#1' ] to deeply equal [ '2bvjuk0tju8', '2bvjuk0tju8#1' ]`（②）
  - `AssertionError: expected 1 to be +0`（③ 老记录不再命中 ⇒ 多发一条）
  - 注码 + 新腿的整包：rc=1，`Test Files 1 failed | 58 passed (59)` / `Tests 3 failed | 766 passed | 7 skipped (776)` ⇒ 红集合恰为这三条，无连带误伤
- `cp` 还原后 md5 = `6af6de63da9ac20a83e6d8ea8ebcbab1` ✓，新腿单跑 `Tests 3 passed (3)` rc=0
- 整包终局 `npx vitest run`：**rc=0，`Test Files 59 passed (59)`，`Tests 769 passed | 7 skipped (776)`**，Duration 13.99s（基线 766+新腿 3）
- `npx eslint test/adapter-dedupkey-shape.test.js` rc=0

### 4. 范围守纪
只碰 `_dedupKey`：未动 `downlink.js` 的 `SentCache`、未动任何 `src/core/` 生产码（162 行现为 `occurrence > 0`，与开工 golden 逐字节一致）、未动 `test/adapter-b24-send-verify.test.js` / `test/downlink-b20d-sentcache-ttl.test.js`。`git status user-web/bridge` 相比开工只多出我这一条未跟踪新文件。日志与备份留在 `/tmp/lane4d-bak/`（`run-mutant-full.txt`、`run-mutant-newlegs.txt`、`run-mutant-full-with-newlegs.txt`、`run-final-full.txt`）。
