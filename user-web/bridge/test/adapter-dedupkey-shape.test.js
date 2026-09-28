// 4-D：`_dedupKey` 的**字符串形状**此前全仓零断言，而它上方的注释把这个形状写成了硬承诺
// （channel-adapter.js:157「_sentKeys 落 localStorage（_sentKey() 键），键形一变就等于全部重报一遍」）。
// 已实跑的否证：把 :162 的 `occurrence > 0` 注成 `>= 0`（首条键形从裸 base 变成 `base#0`），
// `test/adapter-r23-occurrence-identity.test.js` 仍 4 passed、整包仍 766 passed | 7 skipped、rc=0
// —— 那四条腿全打在 `_canonicalMsgId`（:191-197）产出的 event_id 上，`_dedupKey` 只喂
// `_hasSent/_markSent`，它的形状没有任何腿守着。本文件补这三格：
//   ① 正向形状：首条 = 裸内容哈希，第二条起才带 `#<n>`（注码下首条变 `base#0` → 本腿红）
//   ② 落盘形状：`_markSent` 的账经 `stop()` flush 进 localStorage 后，首条记录仍是裸 base
//      （注释里「落 localStorage」那半句就在这格；注码下记录变 `base#0` → 本腿红）
//   ③ 反向半边：老版本落盘的**裸 base** 记录，升级后首扫必须命中它、一条都不许多发。
//      这一格才是注释承诺的那个后果：键形一变 → 查的是 `base#0`、账上记的是 `base` →
//      两个名字不同名 → 存量记录整体失效 → 「全部重报一遍」真的发生（注码下本腿红）。
//
// 为什么不直接抄 `_dedupKey` 的返回值来断：那等于让被测函数自己给自己当标准。
// 期望键在这里按**契约**拼出来 —— 内容维度稳定键 = `_hash(会话|发送者|文本)`，首条不加任何后缀。
// `_hash` 只是被拿来独立复算这个哈希（它是与 :162 无关的另一格），因此改动拼接口径
// （会话/发送者/文本的顺序或分隔符）同样会红 —— 那同样是「存量记录整体失效」，该红。
import { describe, it, expect, beforeEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';

// _sentKeys 落 localStorage，同一 channel+domain 的键跨用例共用 ⇒ 上一条用例记过的账会把
// 本条用例的首扫吞成 0 条。每例先清空，保证「首扫」是真首扫、老记录是老记录。
beforeEach(() => { localStorage.clear(); });

const CID = 'conv-4d';
const SENDER_ID = 'cust-1';
const TEXT = '好的';

function makeState(texts) {
  return { nodes: texts.map((t) => ({ __text: t })) };
}

function buildAdapter(state, channel = 'douyin_web') {
  return new BaseAdapter({
    name: '4d-dedupkey-shape',
    channel,
    SEL: {},
    hooks: {
      match: () => true,
      getAccountId: () => 'acct-4d',
      getConversationId: () => CID,
      getMessageItems: () => state.nodes,
      parseMessageItem: (node) => {
        if (!node || node.__text === undefined) return null;
        return {
          sender_type: 'customer',
          sender_id: SENDER_ID,
          text: node.__text,
          timestamp: 1,
        };
      },
    },
  });
}

// 契约期望值（独立于 :162 的实现拼出）：首条裸哈希，第 n+1 条带 `#n`。
function expectedKeys(adapter) {
  const base = adapter._hash(`${CID}|${SENDER_ID}|${TEXT}`);
  return { base, first: base, second: `${base}#1`, third: `${base}#2` };
}

describe('4-D `_dedupKey` 键形硬承诺（channel-adapter.js:157 注释）', () => {
  it('① 首条稳定键必须是裸 base，第二条起才带 #<n>', () => {
    const state = makeState([TEXT, TEXT, TEXT]);
    const adapter = buildAdapter(state);
    const { base, second, third } = expectedKeys(adapter);
    adapter.start({ onMessage: () => {} });

    adapter.getMessages();

    // 直接问一次函数：首条（occurrence 缺省即 0）不得出现任何 `#` 后缀。
    const parsed = adapter.parseMessageItem(state.nodes[0]);
    expect(adapter._dedupKey(CID, parsed)).toBe(base);
    expect(adapter._dedupKey(CID, parsed, 0)).toBe(base);
    expect(adapter._dedupKey(CID, parsed, 1)).toBe(second);
    expect(adapter._dedupKey(CID, parsed, 2)).toBe(third);
    // 巡检重访用的账本也必须同形：首条那条账的名字就是裸 base。
    expect([...adapter._sentKeys.keys()]).toEqual([base, second, third]);
    adapter.stop();
  });

  it('② flush 到 localStorage 的首条记录仍是裸 base（注释里「落 localStorage」那半句）', () => {
    const state = makeState([TEXT, TEXT]);
    const adapter = buildAdapter(state);
    const { base, second } = expectedKeys(adapter);
    const storageKey = adapter._sentKey(); // 本腿不验 localStorage 的键名，只验记录里的键形
    adapter.start({ onMessage: () => {} });

    expect(adapter.getMessages().length).toBe(2);
    adapter.stop(); // stop() 内 _saveSentKeys() 落盘

    const raw = localStorage.getItem(storageKey);
    expect(raw).toBeTruthy();
    expect(JSON.parse(raw).map(([k]) => k)).toEqual([base, second]);
  });

  it('③ 反向半边：老落盘的裸 base 记录必须被首扫命中，一条都不许多发', () => {
    const probe = buildAdapter(makeState([TEXT]));
    const { base } = expectedKeys(probe);
    const storageKey = probe._sentKey();
    // 模拟「上一个版本留下的账」：按契约形状（裸 base）写入，不经当前实现的 `_dedupKey`。
    localStorage.setItem(storageKey, JSON.stringify([[base, Date.now()]]));

    const state = makeState([TEXT]);
    const adapter = buildAdapter(state);
    adapter.start({ onMessage: () => {} }); // 内里 _loadSentKeys() 读回上面那条

    // 键形没变 ⇒ 查的名字与账上的名字同名 ⇒ 这条早报过，必须被吞掉。
    // 键形一变（首条变 `base#0`）⇒ 账上的裸 base 再也不命中 ⇒ 全部重报一遍（本腿该红）。
    expect(adapter.getMessages().length).toBe(0);
    adapter.stop();
  });
});
