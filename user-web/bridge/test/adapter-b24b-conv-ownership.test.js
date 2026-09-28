// 批24b（§8.3-8 的连带发现）：`getMessageItems()` 的「会话归属校验」必须六条迭代路径同一条口径。
//
// 批24 给回查（`_countVisibleText`）加 root.contains 时，读码数出这个类里一共六处在迭代
// `getMessageItems()` 的返回值：getMessages / _collectUnseenText / _countVisibleText 这三条
// 当时已经判了归属，另外三条没有：
//   · _backfill          —— 上一会话残留气泡会被并进**当前**会话的历史帧上行（串台）；
//   · _handleIncremental —— _scanIncremental 每 3s 走这条，同样把残留增量上行；
//   · _occurrenceInList  —— 残留节点排在真实节点前面时，真实那条的发生次数被 +1，
//                           规范键从 mh:<hash> 漂成 mh:<hash>#1，与 §8.3-17 的编号口径互相打脸。
// 三条都是既有代码，与批24 同源同一条判据 ⇒ 一起收，不留「已知但没修」。
// 那三条「已经有判据」的同样各钉一条腿（_countVisibleText 那条在 adapter-b24-send-verify 里）：
// 判据有没有牙由 scripts/mut_send_verify_b24.py 的 V12–V16 逐格注码证明 —— 摘掉哪一处，
// 红必须恰好落在绑定的那一条腿上（多红少红都算 BROKEN）。
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';

beforeEach(() => { localStorage.clear(); });

function makeState({ inRoot = [] } = {}) {
  const root = document.createElement('div');
  const strayBox = document.createElement('div');
  const put = (box, text) => {
    const el = document.createElement('div');
    el.dataset.msg = '1';
    el.textContent = text;
    box.appendChild(el);
    return el;
  };
  inRoot.forEach((t) => put(root, t));
  return {
    root,
    addBubble: (text) => put(root, text),
    addStrayBubble: (text) => put(strayBox, text),
    // 文档顺序：残留容器在前、当前会话容器在后（虚拟列表切换后两种排布都出现过）。
    items: () => [...strayBox.querySelectorAll('[data-msg]'), ...root.querySelectorAll('[data-msg]')],
  };
}

function buildAdapter(state) {
  return new BaseAdapter({
    name: 'b24b',
    channel: 'douyin_web',
    SEL: {},
    hooks: {
      match: () => true,
      getAccountId: () => 'acct-b24b',
      getConversationId: () => 'conv-b24b',
      getMessageItems: state.items,
      getMessageRoot: () => state.root,
      parseMessageItem: (node) => (
        node && node.textContent
          ? { sender_type: 'customer', sender_id: 'cust-1', text: node.textContent, timestamp: 1 }
          : null
      ),
      sendText: () => Promise.resolve(),
    },
  });
}

const textsOf = (frames) => frames.flatMap((f) => (
  (f.history && f.history.length ? f.history.map((h) => h.content) : [f.content])
));

describe('批24b：六条 getMessageItems 迭代路径共用同一条会话归属判据', () => {
  it('_backfill 不得把上一会话残留气泡并进当前会话的历史帧', async () => {
    const state = makeState({ inRoot: ['屏内的这条'] });
    state.addStrayBubble('上一会话的残留');
    const onMessage = vi.fn();
    const adapter = buildAdapter(state);
    expect(adapter.start({ onMessage })).toBe(true);

    const texts = textsOf(onMessage.mock.calls.map((c) => c[0]));
    expect(texts).toContain('屏内的这条');
    expect(texts).not.toContain('上一会话的残留');
    adapter.stop();
  });

  it('_scanIncremental（fallbackTimer 每 3s 走这条）不得增量上行残留气泡', async () => {
    const state = makeState({ inRoot: ['屏内的这条'] });
    const adapter = buildAdapter(state);
    const onMessage = vi.fn();
    expect(adapter.start({ onMessage })).toBe(true);
    // 残留是「切换之后」才留在文档里的，回填时它还不存在 ⇒ 只能由增量路径处理。
    state.addStrayBubble('切换后残留');
    onMessage.mockClear();

    adapter._scanIncremental();

    // 屏内那条已由回填登记过 ⇒ 这一轮唯一"没见过"的节点就是残留。修好后必须一条都不上行。
    expect(onMessage).not.toHaveBeenCalled();
    adapter.stop();
  });

  it('_occurrenceInList 只数本会话的节点：残留不得把真实那条挤成 #1', async () => {
    const state = makeState({ inRoot: [] });
    const adapter = buildAdapter(state);
    expect(adapter.start({ onMessage: vi.fn() })).toBe(true);

    const real = state.addBubble('同一句话');
    state.addStrayBubble('同一句话'); // 排在 items 前面（文档顺序）
    const parsed = adapter.parseMessageItem(real);
    expect(adapter._occurrenceInList(real, 'conv-b24b', parsed)).toBe(0);
    adapter.stop();
  });

  it('getMessages（PollingLoop 每秒走的公开入口）不得把残留节点当本会话新消息返回', async () => {
    const state = makeState({ inRoot: ['屏内的这条'] });
    state.addStrayBubble('上一会话的残留');
    const adapter = buildAdapter(state);
    expect(adapter.start({ onMessage: vi.fn() })).toBe(true);
    adapter._sentKeys.clear(); // 回填已把屏内那条登记为已发；本腿只验归属，不验去重

    const texts = adapter.getMessages().map((m) => m.text);
    expect(texts).toContain('屏内的这条');
    expect(texts).not.toContain('上一会话的残留');
    adapter.stop();
  });

  it('_collectUnseenText（巡检单个会话的一抓）不得把残留节点收进批量上行', async () => {
    const state = makeState({ inRoot: ['屏内的这条'] });
    const adapter = buildAdapter(state);
    expect(adapter.start({ onMessage: vi.fn() })).toBe(true);
    // 残留在回填之后才出现在文档里 ⇒ 「未见过」的集合里只有它，正是巡检会误抓的那一条。
    state.addStrayBubble('切换后残留');

    const batch = adapter._collectUnseenText();
    // 返回的是 parsed 本体（不是 {parsed} 包装），这里取错形状会让 V16 的红因变成
    // `Cannot read properties of undefined` —— 杀是真的杀了，但读不出"因为残留被收进来了"。
    expect(batch.map((p) => p.text)).toEqual([]);
    adapter.stop();
  });
});
