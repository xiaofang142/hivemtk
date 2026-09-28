// §8.3-8（批24）：B 链路「点了 = 已送达」这个假 ✓ 必须收掉。
//
// 改前实测（读码取证）：channel-adapter.js::sendOutbound 里 `ok` 唯一来源是
// 「rawSendText 没抛、没超时」，紧接着就 `rateLimiter.markSent` + `_emitMessage(SENDER.AGENT,…)`
// 把这条**声称已发出**的消息上行入库，再往下 downlink 无条件 ack delivered。
// 也就是说：平台把这条静默吞掉（风控/审核/按钮 disabled 被事件吞）时，服务端仍会有一行
// 「已发出」，与批20b 在 A 链路消灭的那类假 ✓ 完全同形，只是换了链路。
//
// 本批改判据：发送后回 DOM 数一次自己的气泡（内容计数**增加**才算，见第 3 条用例）。
// 两条红线：
//   ① 回查结论**不参与** ack —— 回查未见也必须照常 ack，因为「点了没见着」不等于「没点出去」，
//      让它触发重投就是把不可撤销的双发交回给一次渲染滞后；
//   ② 回查是只读 —— 绝不允许推进 _sentKeys / occurrence 计数，否则回查本身会把该上行的
//      气泡吃掉（与 §8.3-17 的身份分层直接冲突）。
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';

beforeEach(() => { localStorage.clear(); });

// 气泡必须是**真实 DOM 节点**：start() 会对 getMessageRoot() 的返回值挂 MutationObserver，
// 传普通对象会让 start() 在 _attachConversation 里抛掉并被 catch 吞成一句 warn —— 首版就栽在
// 这儿：七条用例全绿，跑的却是一个启动到一半就失败的适配器（回填/巡检全没起来）。
// root = 当前会话的消息容器；strayBox = 虚拟列表切换后仍留在文档里的上一会话容器（root 之外）。
function makeState({ initial = [] } = {}) {
  const root = document.createElement('div');
  const strayBox = document.createElement('div');
  const put = (box, text) => {
    const el = document.createElement('div');
    el.dataset.msg = '1';
    el.textContent = text;
    box.appendChild(el);
    return el;
  };
  initial.forEach((t) => put(root, t));
  return {
    root,
    addBubble: (text) => put(root, text),
    addStrayBubble: (text) => put(strayBox, text),
    // 真实适配器里这是一条文档级查询：上一会话的残留节点同样会被捞出来，
    // 再由 root.contains 判归属剔除 —— 回查与 getMessages 必须共用这一条口径。
    items: () => [...root.querySelectorAll('[data-msg]'), ...strayBox.querySelectorAll('[data-msg]')],
  };
}

function buildAdapter(state, { sendDelayMs = 0, sendTextOverride = null } = {}) {
  return new BaseAdapter({
    name: 'b24',
    channel: 'douyin_web',
    SEL: {},
    hooks: {
      match: () => true,
      getAccountId: () => 'acct-b24',
      getConversationId: () => 'conv-b24',
      getMessageItems: state.items,
      getMessageRoot: () => state.root,
      parseMessageItem: (node) => {
        if (!node || !node.textContent) return null;
        // 自/他判定已移交后端 ⇒ 前端读到的每条都是 customer；回查判据不得依赖自/他。
        return { sender_type: 'customer', sender_id: 'cust-1', text: node.textContent, timestamp: 1 };
      },
      sendText: sendTextOverride || (() => {
        if (sendDelayMs > 0) {
          setTimeout(() => { state.addBubble('在的'); }, sendDelayMs);
          return Promise.resolve();
        }
        state.addBubble('在的');
        return Promise.resolve();
      }),
    },
  });
}

const agentFrames = (onMessage) => onMessage.mock.calls.map((c) => c[0]).filter((f) => f.sender_type === 'agent');

describe('§8.3-8 出站后回 DOM 复核自己的气泡（批24）', () => {
  it('气泡真的出现 → sendVerified=true，且 AGENT 回声帧带 extra.send_verified=true', async () => {
    const state = makeState({ initial: ['你好'] });
    const adapter = buildAdapter(state);
    const onMessage = vi.fn();
    expect(adapter.start({ onMessage })).toBe(true);

    const res = await adapter.sendOutbound('在的', undefined, { sendVerifyMs: 600 });

    expect(res.ok).toBe(true);
    expect(res.sendVerified).toBe(true);
    const frames = agentFrames(onMessage);
    expect(frames).toHaveLength(1);
    expect(frames[0].extra).toBeTruthy();
    expect(frames[0].extra.send_verified).toBe(true);
    adapter.stop();
  });

  it('反向半边：平台静默吞（气泡始终不出现）→ 照常 ack 语义不变，但结论位必须是 false', async () => {
    const state = makeState({ initial: ['你好'] });
    const adapter = buildAdapter(state);
    const swallow = vi.spyOn(adapter, 'rawSendText').mockImplementation(() => Promise.resolve());
    const onMessage = vi.fn();
    expect(adapter.start({ onMessage })).toBe(true);

    const res = await adapter.sendOutbound('在的', undefined, { sendVerifyMs: 300 });

    // ① 判据本身：没见着就是没见着，不许跟着 ok 一起绿
    expect(res.ok).toBe(true);
    expect(res.sendVerified).toBe(false);
    // ② 红线：回查未见**不得**改 ack/重发语义（防双发优先于防假绿）
    expect(swallow).toHaveBeenCalledTimes(1);
    const frames = agentFrames(onMessage);
    expect(frames).toHaveLength(1);
    expect(frames[0].extra.send_verified).toBe(false);
    adapter.stop();
  });

  // 红线①的下半句（channel-adapter.js:1265-1266「不碰 markSent」）要各自的腿：
  // 上面的「反向半边」钉的是 ok/回声帧，rawSendText 计数不区分「第二发穿过限流层」这一支。
  // 变异杀：把 :1268 的 `this.rateLimiter.markSent(...)` 包进 `if (sendVerified) { … }` ——
  // 未见即不记内容去重账，下一条同文本在限流层直接放行「真发第二遍」，
  // 结论位就成了 §8.3 行 8 明确拒绝的第二道发送闸（双发不可撤销，判据只许产结论）。
  it('红线①下半句：回查未见也必须记 markSent 账 —— 第二发同文本仍被内容去重层短路（杀：markSent 被 if (sendVerified) 门控）', async () => {
    const state = makeState({ initial: ['你好'] });
    const adapter = buildAdapter(state);
    const swallow = vi.spyOn(adapter, 'rawSendText').mockImplementation(() => Promise.resolve());
    const markSent = vi.spyOn(adapter.rateLimiter, 'markSent');
    const onRateLimited = vi.fn();
    expect(adapter.start({ onMessage: vi.fn(), onRateLimited })).toBe(true);

    const r1 = await adapter.sendOutbound('在的', undefined, { sendVerifyMs: 300 });
    expect(r1.ok).toBe(true);
    expect(r1.sendVerified).toBe(false); // 前提：本腿确实跑在「未见」分支上
    // 未见不碰 markSent：结论位不得往下漏到去重账（变异在此即红：调用次数 0）
    expect(markSent).toHaveBeenCalledTimes(1);
    expect(markSent).toHaveBeenCalledWith('douyin_web', 'acct-b24', 'conv-b24', '在的');

    // 时钟拨过 minInterval(1500ms)/cooldown(3000ms)、留在 dedup 窗(60s) 内：
    // 第二发若被拦，只剩「内容去重」一层拦得动 —— 断言不与节奏层纠缠。
    const realNow = Date.now;
    Date.now = () => realNow() + 5000;
    let r2;
    try {
      r2 = await adapter.sendOutbound('在的', undefined, { sendVerifyMs: 300 });
    } finally {
      Date.now = realNow;
    }
    // 与「回查落地」时同形的出路：限流层判重 → rateLimited，rawSendText 全程只跑这一次
    expect(r2.ok).toBe(false);
    expect(r2.rateLimited).toBe(true);
    expect(swallow).toHaveBeenCalledTimes(1);
    const reasons = onRateLimited.mock.calls.map((c) => c[0].reason);
    expect(reasons.some((s) => s.includes('dedup same text'))).toBe(true);
    adapter.stop();
  }, 15000);

  it('基线判据：发送前窗口里就有一条同文本（客户先说过）→ 只"存在"不算数', async () => {
    // 用「计数增加」而不是「文本命中」：否则客户刚说过同一句话时，我们一次失败的发送
    // 会因为他人的气泡被判成已送达 —— 那比不判更糟。
    const state = makeState({ initial: ['在的'] });
    const adapter = buildAdapter(state);
    vi.spyOn(adapter, 'rawSendText').mockImplementation(() => Promise.resolve());
    expect(adapter.start({ onMessage: vi.fn() })).toBe(true);

    const res = await adapter.sendOutbound('在的', undefined, { sendVerifyMs: 300 });

    expect(res.ok).toBe(true);
    expect(res.sendVerified).toBe(false);
    adapter.stop();
  });

  it('渲染滞后容得下：气泡在 150ms 后才挂上 → 回查轮询要等到它', async () => {
    const state = makeState({ initial: ['你好'] });
    const adapter = buildAdapter(state, { sendDelayMs: 150 });
    expect(adapter.start({ onMessage: vi.fn() })).toBe(true);

    const res = await adapter.sendOutbound('在的', undefined, { sendVerifyMs: 2000 });

    expect(res.sendVerified).toBe(true);
    adapter.stop();
  });

  it('跨会话残留节点不计入：点击之后才挂进 items 的上一会话同文本气泡，不算这次发出去了', async () => {
    // 残留必须出现在**点击之后**：虚拟列表正是在切换/发送的间隙把上一会话节点补进
    // getMessageItems() 的。若残留早在基线里，基线和计数会一起 +1、对称抵消，
    // 这条腿就成了一条没有牙的断言（去掉 root.contains 也照样绿）。
    const state = makeState({ initial: ['你好'] });
    const adapter = buildAdapter(state, {
      sendTextOverride: () => {
        state.addStrayBubble('在的'); // 平台吞了这条，文档里只长出上一会话的残留
        return Promise.resolve();
      },
    });
    expect(adapter.start({ onMessage: vi.fn() })).toBe(true);

    const res = await adapter.sendOutbound('在的', undefined, { sendVerifyMs: 300 });

    expect(res.ok).toBe(true);
    expect(res.sendVerified).toBe(false);
    adapter.stop();
  });

  it('红线②：回查是只读 —— 不写去重容器，之后正常扫描仍能报出这些气泡', async () => {
    const state = makeState({ initial: ['你好'] });
    const adapter = buildAdapter(state);
    expect(adapter.start({ onMessage: vi.fn() })).toBe(true);
    // 只取 start 建立的状态（账号/会话/_sentKeys 装载），随即关掉回填与巡检定时器：
    // grace 回填挂在 1500ms、而 sendOutbound 里的全局最小间隔闸本身就能睡到 1500ms，
    // 留着它们断言就是和定时器赛跑（回填把气泡标成已上报是合法行为，不是本腿要证的缺陷）。
    adapter.stop();

    const res = await adapter.sendOutbound('在的', undefined, { sendVerifyMs: 600 });
    expect(res.sendVerified).toBe(true);

    // 回查数的是同一批节点：它若顺手 _markSent / seenNodes.add，本该上行的那条就被自己吃掉了
    // （与 §8.3-17 的发生次数身份直接冲突）。快照打在 sendOutbound 之后 —— 回声帧自己那笔账不在本腿范围。
    const keysBefore = [...adapter._sentKeys.keys()];
    // seenNodes 是 WeakSet（不可枚举）⇒ 逐节点快照布尔位再比。
    const seenBefore = new Map(state.items().map((n) => [n, adapter.seenNodes.has(n)]));
    expect(await adapter._verifySendLanded('在的', 0, 50)).toBe(true);
    expect(await adapter._verifySendLanded('屏外没有的这条', 0, 50)).toBe(false);
    expect([...adapter._sentKeys.keys()]).toEqual(keysBefore);
    expect(new Map(state.items().map((n) => [n, adapter.seenNodes.has(n)]))).toEqual(seenBefore);

    // 行为面：回查看过的那条自己发的消息，仍必须被正常扫描报出来。
    expect(adapter.getMessages().map((m) => m.text)).toContain('在的');
  });

  it('三态：没走到回查的出路（目标会话打不开）不带结论位，既不是 true 也不是 false', async () => {
    const state = makeState({ initial: ['你好'] });
    const adapter = buildAdapter(state);
    expect(adapter.start({ onMessage: vi.fn() })).toBe(true);

    const res = await adapter.sendOutbound('在的', 'ghost-conv', { sendVerifyMs: 600 });

    expect(res.ok).toBe(false);
    expect('sendVerified' in res).toBe(false);
    adapter.stop();
  });
});
