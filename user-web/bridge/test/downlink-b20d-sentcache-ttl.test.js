// 批20d-A4（2026-09-21）：SentCache 加 24h 时间界。
//
// 立项依据（§8.3 行 4 / 调研同行口径）：本地已发缓存持久化到 chrome.storage.local，
// 但**只有条数上限（sentCacheMax:2000）、没有时间界** —— 一条 msg_id|conversation_id 一旦写入
// 就永久占位（直到被插入序挤出 2000 名之外）。两个后果：
//   ① 存储单调膨胀且永不回收（2000 条里绝大部分是三周前发过的，早已不可能再被服务端重推）；
//   ② 更要命的是「永久」这件事本身没有依据：服务端重推窗是有限的（批20d-A3 之后有上界），
//      界之后的记录既不再防任何重复，却继续占着名额、把真正还可能重推的新记录从尾部挤掉。
// 故加 TTL。取值 **24h 而不是同行常用的 5min**：界必须 ≥ 上游仍能重投的时长，
// 否则「过期即重发」反而放大重复风险（E 格把这条下界钉成断言，防止有人日后"优化"成分钟级）。
//
// 手法沿用本目录既有测试：真 downlink 模块 + 内存版 chrome.storage.local，唯一替身是 HTTP 层。
// 关键点是**不从内部戳 Set/Map**，而是"先写好一份带时间戳的存量记录，再跑一轮真轮询"，
// 让 TTL 的表现落在可观测的行为面上（发或不发、存储里剩哪些键）。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

function makeChromeStorage() {
  const data = {};
  return {
    storage: {
      local: {
        get: vi.fn((keys) => {
          const out = {};
          const arr = Array.isArray(keys) ? keys : [keys];
          for (const k of arr) if (k in data) out[k] = data[k];
          return Promise.resolve(out);
        }),
        set: vi.fn((obj) => { Object.assign(data, obj); return Promise.resolve(); }),
      },
    },
    runtime: { sendMessage: vi.fn(async () => {}) },
    _data: data,
  };
}

vi.mock('../src/core/http-ingest.js', async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, getOutbox: vi.fn(), ackOutbox: vi.fn() };
});
vi.mock('../src/core/sanitize.js', () => ({
    sanitizeForDisplay: (t) => t,
    stripMarkdownForDM: (t) => t,
  }));

import { pollDownlink, initDownlink } from '../src/core/downlink.js';
import { getOutbox, ackOutbox } from '../src/core/http-ingest.js';
import { BRIDGE_THREE_CHANNEL } from '../src/core/constants.js';

const cfg = () => async () => ({ serverUrl: 'http://localhost:8204', token: 't' });
const HOUR = 3600_000;
const storedKeys = (ch) => (chrome._data[`bridge_sent_${ch}`] || []).map((e) => (Array.isArray(e) ? e[0] : e));

describe('批20d-A4：SentCache 的 24h 时间界', () => {
  let st;
  beforeEach(() => { st = makeChromeStorage(); globalThis.chrome = st; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  // A. 界内照旧拦：23h 前发过的行，服务端仍列进待办 → 不许重发，只补确认。
  //    这一格是"别把修复做成回归"的下界：TTL 不能把还在重推窗内的记录提前放开。
  it('A 界内（23h）命中：不重发，只补一次 delivered', async () => {
    st._data.bridge_sent_b20da = [['m-keep|c1', Date.now() - 23 * HOUR]];
    await initDownlink(['b20da']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-keep', content: '重复', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b20da', 'acc1', cfg(), { sendOutbound });
    expect(sendOutbound).not.toHaveBeenCalled();
    expect(ackOutbox).toHaveBeenCalledTimes(1);
    expect(ackOutbox.mock.calls[0][1]).toEqual(['m-keep']);
  });

  // B. 界外不再拦：25h 前发过的行仍在待办里 → 判为「这份记录已经不代表任何在途重推」，
  //    照常下发（真发一次），并把这条记录以新时间戳续上（不是删完就走，否则同一条永远在「过期→重发」抖动）。
  it('B 界外（25h）不再拦：重发一次，且记录以新时间戳续期', async () => {
    st._data.bridge_sent_b20db = [['m-aged|c1', Date.now() - 25 * HOUR]];
    await initDownlink(['b20db']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-aged', content: '补发', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    const before = Date.now();
    await pollDownlink('b20db', 'acc1', cfg(), { sendOutbound });
    expect(sendOutbound).toHaveBeenCalledTimes(1);
    const rows = (st._data.bridge_sent_b20db || []).map((e) => (Array.isArray(e) ? e : [e, 0]))
      .filter(([k]) => k === 'm-aged|c1');
    expect(rows).toHaveLength(1);
    expect(rows[0][1]).toBeGreaterThanOrEqual(before);
  });

  // C. 超上限按「最旧的时间戳」淘汰，而不是按插入序。
  //    （旧实现 [...set].slice(len-max) 是插入序截尾——Set 里命中已有键不刷新位置，
  //     于是「今天还在被重推的新记录」可能因为插入位靠前被挤掉，反而三条里最先丢的是最该留的。）
  //    夹具的关键是**插入序与时间序故意不一致**：最新的那条最先写入、最旧的那条最后写入。
  //    第一版没做这个错位（seed 按 i 递增 ⇒ 两种序长得一样），于是「按插入序淘汰」这个
  //    本批立项要修的旧行为在原断言下照样全绿——变异电池跑出来是存活，才逼出这一版夹具。
  it('C 上限淘汰按时间：插入序与时间序错位时，丢的是最旧的而不是最先写入的', async () => {
    const max = BRIDGE_THREE_CHANNEL.sentCacheMax;
    const base = Date.now() - 5 * HOUR; // 除下面那条外全部在界内
    const seed = [['k-newest|c1', base + 10000]]; // 时间上最新，插入上最前
    for (let i = 0; i < max; i++) seed.push([`k${i}|c1`, base + i]);
    seed.push(['k-expired|c1', Date.now() - 30 * HOUR]); // 时间上最旧，插入上最后
    expect(seed.length).toBe(max + 2);
    st._data.bridge_sent_b20dc = seed;
    await initDownlink(['b20dc']);
    // 必须真下发一条：落盘只在 dirty 时发生（flush 见 SentCache#flush），空轮询不写存储，
    // 断言就会打在「装载前的原始 seed」上——那时长度、内容全都对得上，等于没断。
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-new', content: '新', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b20dc', 'acc1', cfg(), { sendOutbound });

    const keys = storedKeys('b20dc');
    expect(keys.length).toBeLessThanOrEqual(max);
    expect(keys).not.toContain('k-expired|c1'); // 过期即回收，不占名额
    expect(keys).not.toContain('k0|c1');        // 时间上第二旧的先走
    expect(keys).toContain('k-newest|c1');      // 插入序第一、时间序最后——按插入序淘汰就会丢它
    expect(keys).toContain(`k${max - 1}|c1`);
    expect(keys).toContain('m-new|c1');
  });

  // H. add() 的「先删后设」本身也有腿（§8.3 行 4 的「按最后命中序」半边）。
  //    变异杀：删掉 add() 里的 `this.mem.delete(id)`（downlink.js:63）只留 set ——
  //    重命中条目就地刷时间戳、不挪插入位，evict 的「头部前缀即过期」快路径前提碎裂，
  //    排在重命中条目**之后**的界外尾条永远回收不掉（淘汰退回按插入序，正是 A4 要改掉的旧行为）。
  //    B/C/G 都够不到这条分支：B 的条目在 load()+evict 已清掉、C 的序被 load() 的 sort 定死、
  //    G 重命中之后没有留在尾巴上的过期条目可观测。
  //    本格三要件：(a) 条数远低于 sentCacheMax ⇒ 回收只可能出自 TTL 前缀循环；
  //    (b) 重命中条目插入序在两条界外尾条之前；(c) 断言重命中条目被续期留在盘上、尾条被回收，
  //    即 evict 按「最后命中时间」而不是「插入时间」裁。
  it('H 会话内重命中续期：evict 按最后命中时间裁，落在重命中条目之后的界外尾条必须回收（杀：add 去掉先删）', async () => {
    const now0 = Date.now();
    // 装载后内存序（load 按 ts 升序排）：m-rehit(-2h) → k-tail1(-1h) → k-tail2(-30min)
    st._data.bridge_sent_b20dh = [
      ['m-rehit|c1', now0 - 2 * HOUR],
      ['k-tail1|c1', now0 - HOUR],
      ['k-tail2|c1', now0 - 30 * 60_000],
    ];
    await initDownlink(['b20dh']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-rehit', content: '重命中', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));

    const realNow = Date.now;
    Date.now = () => realNow() + BRIDGE_THREE_CHANNEL.sentCacheTtlMs + HOUR; // +25h：三条种子全部越界
    try {
      await pollDownlink('b20dh', 'acc1', cfg(), { sendOutbound });
    } finally {
      Date.now = realNow;
    }
    // 前提成立：这一轮真走了「has 判越界 → 重发 → add() 命中已有键」这条分支
    expect(sendOutbound).toHaveBeenCalledTimes(1);

    const rows = (st._data.bridge_sent_b20dh || []).filter((e) => Array.isArray(e));
    const keys = rows.map((e) => e[0]);
    // 重命中条目按最后命中时间续期并留在盘上（它才是「今天还在被重推」的那条）
    const rehit = rows.find((e) => e[0] === 'm-rehit|c1');
    expect(rehit).toBeTruthy();
    expect(rehit[1]).toBeGreaterThan(now0);
    // 界外尾条必须被 TTL 前缀循环回收：它们插在后头，add 若不在命中时挪位，
    // evict 的 break 会停在原地续期的重命中条目上，这两条就永远赖着不走（变异红点）。
    expect(keys).not.toContain('k-tail1|c1');
    expect(keys).not.toContain('k-tail2|c1');
  });

  // F. 装载即回收**不依赖条数上限**：条目没超界时，只有时间界会把过期记录清掉。
  //    C 的夹具是超界的（截尾顺手也能清掉过期那条），所以「删掉 evict 里的过期前缀循环」
  //    在 C 上不红；这一界才真正守着它——而它对应的是 A4 立项理由①（存储单调膨胀、永不回收）。
  it('F 未超上限也回收过期条目：时间界不是条数上限的副产品', async () => {
    const max = BRIDGE_THREE_CHANNEL.sentCacheMax;
    const base = Date.now() - 5 * HOUR;
    const seed = [['k-expired|c1', Date.now() - 30 * HOUR]];
    for (let i = 1; i < max - 1; i++) seed.push([`k${i}|c1`, base + i]); // 故意留出没超界的余量
    expect(seed.length).toBeLessThan(max);
    st._data.bridge_sent_b20df = seed;
    await initDownlink(['b20df']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-new', content: '新', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b20df', 'acc1', cfg(), { sendOutbound }); // 这条 m-new 会触发落盘

    const keys = storedKeys('b20df');
    expect(keys).toContain('m-new|c1');
    expect(keys).toContain('k1|c1');
    expect(keys).not.toContain('k-expired|c1'); // 没超界 ≠ 该留：界外的记录既不防重推也占着存储
  });

  // D. 升级不丢存量：老版本写的是纯字符串数组（无时间戳）。这份记录的真实年龄未知，
  //    判成「已过期」等于把升级瞬间在途的重复消息全放出去（不可撤回），所以装载时以当前时间续期。
  it('D 旧格式（纯字符串）可读：不重发，并被改写为带时间戳的新格式', async () => {
    st._data.bridge_sent_b20dd = ['m-legacy|c1'];
    await initDownlink(['b20dd']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-legacy', content: '旧记录', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b20dd', 'acc1', cfg(), { sendOutbound });
    expect(sendOutbound).not.toHaveBeenCalled();
    const raw = st._data.bridge_sent_b20dd || [];
    expect(raw.every((e) => Array.isArray(e) && e.length === 2 && typeof e[1] === 'number')).toBe(true);
  });

  // E. TTL 的下界是一条设计约束而非实现细节：< 上游重推窗 = 反而放大重复。
  it('E TTL 下界锁：≥24h（同行 5min 去重窗不可照抄，界必须盖住服务端重推窗）', () => {
    expect(BRIDGE_THREE_CHANNEL.sentCacheTtlMs).toBeGreaterThanOrEqual(24 * HOUR);
  });

  // I. 落盘内容必须**恰好**是 TTL 内存活的那一批，且必须是时间升序
  //    （A~H 只逐条断言 contain / not.toContain，从没钉过「落盘集合 = 存活集合」这个整体形状，
  //    也就从没钉过支撑 evict 快路径的那个顺序前提）。
  //
  //    变异电池实测（每刀单独注一次，括号里是该刀下红的用例；J 加入前的九条只在 ④ 上集体无牙）：
  //      ① 摘掉 load() 末尾的重排(:47)          → C、I、J 红
  //      ② evict 的回收边界往前收(ttl 折半)      → A、I、J 红
  //      ③ 摘掉 evict 里的整段 TTL 过滤          → F、H、I、J 红
  //      ④ 落盘按时间**降序**写回                → **只有 I 红**（本格的独牙，九条老腿全绿）
  //      ⑤ 落盘只写「最近一批」(漏掉仍活的旧条目) → C、F、I、J 红
  //    ①②③⑤ 是「别家也拦得住、本格再拦一遍」的重复保险，如实登记、不当新增覆盖面卖；
  //    真正只有本格守着的是 ④：evict 用的是「过期项必是头部前缀」这条线性快路径
  //    （`:69-75` 的 `break`），落盘顺序一旦不再是时间升序，下一次装载扫到第一个存活项就收工，
  //    排在它后面的界外条目**每一轮都扫不到**，永久占着 sentCacheMax 名额 —— A4 立项理由①。
  //    夹具三处缺一不可：**存储是预置的**（不预置就看不出装载时对存量的处置），
  //    老格式排在最前（只有它挡在过期项前面才能暴露①；H 的重命中条目排在过期项**后面**，
  //    露出的是 add() 少一次 delete 那一刀，两格合起来才把「头部前缀」这个前提的两端都钉住），
  //    界内那条取 23h（贴近界而不越界，任何"往前收边界"的改法都会把它写丢）。
  //    跑三轮 add()：落盘逐轮基于上一轮结果改写，一轮看不出「旧内容被带回」。
  it('I 落盘=恰好 TTL 存活集（混排存量 + 多轮 add）：界外的走、界内的留，且写回仍是时间升序', async () => {
    const t0 = Date.now();
    st._data.bridge_sent_b20di = [
      'k-legacy|c1',                     // 老格式（无时间戳）：装载时按 now 续期，位置在最前
      ['k-expired|c1', t0 - 30 * HOUR],  // 界外：必须从落盘内容里消失
      ['k-pending|c1', t0 - 23 * HOUR],  // 界内（23h < 24h）：必须原样留着，且不得被续期改写
    ];
    await initDownlink(['b20di']);
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    for (const id of ['m-r1', 'm-r2', 'm-r3']) {
      getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: id, content: id, conversation_id: 'c1' }] });
      await pollDownlink('b20di', 'acc1', cfg(), { sendOutbound });
    }
    expect(sendOutbound).toHaveBeenCalledTimes(3);

    const rows = st._data.bridge_sent_b20di || [];
    // 形状：写回的每一条都必须带时间戳，否则下次装载又要走「年龄未知→续一整期」那条兜底
    expect(rows.every((e) => Array.isArray(e) && e.length === 2 && Number.isFinite(e[1]))).toBe(true);
    // 集合：不多不少。多 = ①（界外的被写回）；少 = 丢存活记录或只写回本批新增。
    expect(storedKeys('b20di').slice().sort()).toEqual(
      ['k-legacy|c1', 'k-pending|c1', 'm-r1|c1', 'm-r2|c1', 'm-r3|c1'].sort());
    // 顺序：落盘必须是时间升序——evict 的「头部即最旧」快路径就建在这个前提上。
    const ts = rows.map(([, t]) => t);
    expect(ts).toEqual(ts.slice().sort((a, b) => a - b));
    // 那条 23h 的记录不得被"顺手续期"：续期等于把回收窗往后推，界就此形同虚设。
    expect(rows.find(([k]) => k === 'k-pending|c1')[1]).toBe(t0 - 23 * HOUR);
  });

  // J. 装载即回收：不依赖「这一轮之后还有没有人发消息」。
  //    杀掉的变异：摘掉 load() 末尾那次 `this.evict(now)`（downlink.js:51）。
  //    实测 A~I 九条对这一刀集体无牙（注刀跑过一遍，红的只有本格）：A/B/G 靠 has()，
  //    C/F/H/I 都真发过消息，add() 里那次 evict 顺手把头部的过期前缀清了，
  //    于是「装载不回收」这件事只在**一条都没再发**的那轮才看得见。
  //    （反向不成立：本格对 I 注过的 ①②③⑤ 四刀也一起红，那是重复保险，不另计覆盖面。）
  //    而这一格正是 A4 立项理由①的另一半：界外的记录既不防任何在途重推、又永久占着
  //    sentCacheMax 名额 ⇒ 回收必须自己成立，不能指望后面恰好有人发消息来顺带清。
  //    夹具沿用 C/I 的手法：老格式那条排最前，它让装载把 dirty 置真 ⇒ 零下发也会落盘，
  //    断言才打在「装载后的写回内容」上，而不是打在没人碰过的原始 seed 上。
  it('J 装载即回收：界外条目在「这一轮一条都没再发」时也必须从盘上消失（杀：load 少一次 evict）', async () => {
    const t0 = Date.now();
    st._data.bridge_sent_b20dj = [
      'k-legacy|c1',                     // 老格式：装载续成 now 并置 dirty（本轮零下发也会写回）
      ['k-expired|c1', t0 - 25 * HOUR],  // 界外 1 小时：服务端不会再推它，只有时间界清得掉
      ['k-pending|c1', t0 - 23 * HOUR],  // 界内：反向半边，不得把回收做成「装载即清空」
    ];
    await initDownlink(['b20dj']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [] }); // 空轮：全程不会调用 add()
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b20dj', 'acc1', cfg(), { sendOutbound });
    expect(sendOutbound).not.toHaveBeenCalled();

    const keys = storedKeys('b20dj');
    expect(keys).not.toContain('k-expired|c1'); // 摘掉 :51 的 evict ⇒ 它被原样写回（本刀只有这条拦）
    expect(keys).toContain('k-pending|c1');
    expect(keys).toContain('k-legacy|c1');
    expect(keys).toHaveLength(2);
  });

  // G. 同一次会话内的过期（不经装载）。
  //    A/B 都靠装载时的 evict 生效；这一格盯的是另一处判据：has() 自己按当前时间判定。
  //    两处都要有腿，否则「删掉 has() 里的时间比较」在装载路径上完全看不出来（第一版就是
  //    这么活的——装载会先把过期条目清掉，于是界外那条永远走不到 has() 的判据上）。
  //    手法：不装存储，而是**第一轮真发一次**（记录以 now 落进缓存），再把时钟拨过界，
  //    第二轮服务端仍给同一条 → 必须重发。
  it('G 会话内跨过界：不经装载也必须放开重发', async () => {
    await initDownlink(['b20dg']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm-live', content: '第一条', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b20dg', 'acc1', cfg(), { sendOutbound });
    expect(sendOutbound).toHaveBeenCalledTimes(1);

    // 同轮再来：界内，必须仍被拦住（否则 TTL 等于没有）
    await pollDownlink('b20dg', 'acc1', cfg(), { sendOutbound });
    expect(sendOutbound).toHaveBeenCalledTimes(1);

    const realNow = Date.now;
    Date.now = () => realNow() + BRIDGE_THREE_CHANNEL.sentCacheTtlMs + HOUR;
    try {
      await pollDownlink('b20dg', 'acc1', cfg(), { sendOutbound });
    } finally {
      Date.now = realNow;
    }
    expect(sendOutbound).toHaveBeenCalledTimes(2);
  });
});
