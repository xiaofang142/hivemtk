// 批19h 契约锁：服务端 refresh 是**一次性轮换**（service/auth.go:197 刷新即把旧令牌拉黑），
// 所以扩展侧「续期成功」与「这次请求用的是哪把令牌」是两件事，必须分别钉住。
//
// 立项形态：exp 进入最后 1h 时 ensureFreshToken 主动续期，但 apiCall 随后仍拿本地
// auth.token（那把刚被服务端拉黑的旧令牌）发业务请求 → 必然 401 → 兜底分支再用同一把
// 死令牌去 refresh（恒失败）→ saveAuth({username}) 把**存储里刚换来的新令牌**一起抹掉
// → 用户被告知「登录已过期，请在弹窗中重新登录」。即每 24h 到点必发一次，
// 且并发两个在途调用时（一个换来 T2、另一个还握着 T1）同样触发。
//
// 断言全部走真模块 + 仿真服务端（refresh 后旧令牌即不可用），不用「fetch 被调用几次」
// 这种形状锁：调用次数对了但用的是死令牌，本文件必须照样红。

import { describe, it, expect, beforeEach, vi } from 'vitest';

const DAY = 86400;

const jwtFor = (exp, jti) =>
  'h.' + Buffer.from(JSON.stringify({ exp, jti })).toString('base64') + '.s';

// makeServer 复刻服务端真实语义：/api/auth/refresh-token 成功即拉黑旧令牌（一次性轮换），
// 业务接口只认「当前存活」的那把令牌。
function makeServer(initialToken) {
  const live = new Set([initialToken]);
  const used = []; // 每次业务请求实际带出去的令牌
  let seq = 0;

  const reply = (status, body) => ({ status, json: async () => body });

  const fetchMock = vi.fn(async (url, opts = {}) => {
    const header = (opts.headers || {})['Authorization'] || '';
    const sent = header.startsWith('Bearer ') ? header.slice(7) : '';
    const target = String(url);

    if (target.endsWith('/api/auth/refresh-token')) {
      if (!live.has(sent)) return reply(401, { code: 401, message: '令牌已失效' });
      live.delete(sent); // ← 一次性轮换：旧令牌当场作废
      seq += 1;
      const next = jwtFor(Math.floor(Date.now() / 1000) + DAY, seq);
      live.add(next);
      return reply(200, { code: 0, data: { token: next } });
    }

    used.push(sent);
    if (!live.has(sent)) return reply(401, { code: 401, message: '令牌已失效' });
    return reply(200, { code: 0, data: { list: [], total: 0, count: 0 } });
  });

  return {
    fetchMock,
    used,
    tokenAlive: (t) => live.has(t),
    rotations: () => seq,
  };
}

function seedAuth(serverUrl, token, exp) {
  return { token, exp, username: 'u1', serverUrl };
}

describe('批19h：静默续期后必须用换来的新令牌发请求', () => {
  let store;
  beforeEach(() => {
    store = {};
    global.chrome = {
      storage: {
        local: {
          get: (key, cb) => cb({ [key]: store[key] }),
          set: (obj, cb) => { Object.assign(store, obj); cb && cb(); },
        },
      },
      runtime: { lastError: null },
    };
  });

  it('exp 剩余 <1h 触发续期时，业务请求带的是新令牌，调用成功且不清登录态', async () => {
    const now = Math.floor(Date.now() / 1000);
    const oldToken = jwtFor(now + 1800, 0); // 30 分钟后过期 → 命中主动续期窗口
    store.browserAutomationAuth = seedAuth('http://localhost:8204', oldToken, now + 1800);
    const srv = makeServer(oldToken);
    global.fetch = srv.fetchMock;

    const { listTasks } = await import('../src/core/api-client.js');
    const data = await listTasks('limit=1'); // 现状：抛「登录已过期」→ 本行即红

    expect(data).toEqual({ list: [], total: 0, count: 0 });
    expect(srv.used).toHaveLength(1);
    expect(srv.tokenAlive(srv.used[0])).toBe(true); // 发出去的那把必须还活着
    expect(srv.used[0]).not.toBe(oldToken);
    expect(store.browserAutomationAuth?.token).toBeTruthy(); // 兜底不许把新令牌抹掉
  });

  it('两个并发调用共用同一把到期令牌时，输的一方不得清掉赢家的新令牌', async () => {
    const now = Math.floor(Date.now() / 1000);
    const oldToken = jwtFor(now + 1800, 0);
    store.browserAutomationAuth = seedAuth('http://localhost:8204', oldToken, now + 1800);
    const srv = makeServer(oldToken);
    global.fetch = srv.fetchMock;

    const { listTasks, getHostStatus } = await import('../src/core/api-client.js');
    const results = await Promise.allSettled([listTasks('limit=1'), getHostStatus()]);

    for (const [i, r] of results.entries()) {
      expect(r.status, `第 ${i} 个并发调用失败：${r.reason?.message}`).toBe('fulfilled');
    }
    // 契约不是「一个字的请求都不许带着旧令牌发出」——并发下无从预知对手已经轮换，
    // 撞一次 401 再换是这条路径的正常成本。契约是：撞一次就要靠盘上的新令牌补救，
    // 既不许原地打转，更不许把赢家换来的令牌当失效证据抹掉。
    const deadSends = srv.used.filter((t) => !srv.tokenAlive(t));
    expect(deadSends.length, `带已作废令牌的业务请求有 ${deadSends.length} 次（>1 = 没从盘上补救，在原地刷）`).toBeLessThanOrEqual(1);
    expect(srv.tokenAlive(store.browserAutomationAuth?.token), '结束时存储里的令牌必须还活着').toBe(true);
    expect(srv.rotations()).toBeLessThanOrEqual(2); // 允许各试一次，不许连环刷
  });

  it('令牌充裕时不发 refresh（现状即绿，这一腿锁住「别把续期改成每次调用都刷」）', async () => {
    const now = Math.floor(Date.now() / 1000);
    const token = jwtFor(now + DAY, 0);
    store.browserAutomationAuth = seedAuth('http://localhost:8204', token, now + DAY);
    const srv = makeServer(token);
    global.fetch = srv.fetchMock;

    const { listTasks } = await import('../src/core/api-client.js');
    await listTasks('limit=1');

    expect(srv.rotations()).toBe(0);
    expect(srv.used).toEqual([token]);
  });
});
