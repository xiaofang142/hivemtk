// @vitest-environment node
//
// 「测试连接」这一颗按钮的判词（2026-09-29）
//
// 起因：按钮原本只打无鉴权的健康口，于是**凭证填错也报「✓ 服务端可达」**；
// 而 user-server 的 /health 在依赖故障时仍回 HTTP 200（真话写在响应体里，
// internal/router/health.go 走的是 response.ErrorWithBusinessCode(50301)，那个 helper 用的
// 状态码就是 StatusOK），只看 res.ok 同样会把"数据库挂了"的部署报成完全正常。
// 用户接着看到的症状是"扩展连着、一条客户消息也不上来"，而这两件事本可以在点按钮时就当场说清。
// 本文件把「探测请求长什么样」与「用户最终读到哪句 banner」两头都钉住。
import { describe, it, expect, vi } from 'vitest';
import { build } from 'esbuild';
import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const popupEntry = resolve(__dirname, '../src/popup/index.js');

let _bundledPromise = null;
function bundlePopup() {
  if (_bundledPromise) return _bundledPromise;
  _bundledPromise = build({
    entryPoints: [popupEntry],
    bundle: true,
    format: 'iife',
    write: false,
    platform: 'browser',
    target: 'es2020',
    logLevel: 'silent',
  }).then((r) => r.outputFiles[0].text);
  return _bundledPromise;
}

async function loadPopup(fetchMock) {
  const popupSrc = await bundlePopup();
  const elements = {};
  const mk = (id) => {
    if (!elements[id]) {
      elements[id] = {
        id, className: '', textContent: '', innerHTML: '', value: '', placeholder: '', disabled: false,
        children: [], addEventListener: () => {}, focus: () => {}, dispatchEvent: () => {},
        appendChild(n) {
          this.children.push(n);
          if (n && typeof n.textContent === 'string') this.textContent = (this.textContent || '') + n.textContent;
          return n;
        },
      };
    }
    return elements[id];
  };
  const document = {
    getElementById: (id) => mk(id),
    createElement: (tag) => ({ tagName: tag, className: '', textContent: '', appendChild: (n) => n }),
    addEventListener: () => {},
  };
  const window = { __popup: null };
  const ctx = {
    document,
    window,
    chrome: {},
    AbortController,
    fetch: fetchMock || (() => Promise.resolve({ ok: false })),
    setTimeout,
    clearTimeout,
    console,
  };
  const fn = new Function(
    ...Object.keys(ctx),
    `${popupSrc}\nreturn typeof window !== "undefined" ? window.__popup : null;`
  );
  return { exported: fn(...Object.values(ctx)), elements };
}

const SERVER = 'http://localhost:8204';
const CAPS_PATH = '/api/bridge/capabilities';
const TOKEN = 'bridge-secret-abc';

function jsonResponse(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  };
}

describe('凭证探测 probeBridgeCredential / 请求形状', () => {
  it('带 X-Bridge-Token 打 capabilities，且凭证绝不进 URL', async () => {
    const calls = [];
    const fetchMock = vi.fn(async (url, init = {}) => {
      calls.push({ url: String(url), init });
      return jsonResponse({ poll_interval_ms: 1500, sse_enabled: true, sse_heartbeat_ms: 15000 });
    });
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.probeBridgeCredential(SERVER, TOKEN);
    expect(calls).toHaveLength(1);
    expect(calls[0].url).toBe(SERVER + CAPS_PATH);
    expect(calls[0].init.headers['X-Bridge-Token']).toBe(TOKEN);
    // 凭证进 URL 就会落进访问日志与浏览器历史，这一条是硬约束
    expect(calls[0].url).not.toContain(TOKEN);
    expect(r.state).toBe('verified');
    expect(r.sseEnabled).toBe(true);
    expect(r.pollIntervalMs).toBe(1500);
    // 返回值里也不许带出凭证（banner 直接用它拼文案）
    expect(JSON.stringify(r)).not.toContain(TOKEN);
  });

  it('凭证栏为空时不伪造空头：不发该头，401 判成「未填」而不是「填错」', async () => {
    const calls = [];
    const fetchMock = vi.fn(async (url, init = {}) => {
      calls.push({ url: String(url), init });
      return jsonResponse({ code: 'UNAUTHORIZED_2001', message: '缺少 X-Bridge-Token' }, 401);
    });
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.probeBridgeCredential(SERVER, '   ');
    expect(calls[0].init.headers['X-Bridge-Token']).toBeUndefined();
    expect(r.state).toBe('missing');
    expect(r.status).toBe(401);
    expect(r.serverMessage).toBe('缺少 X-Bridge-Token');
  });

  it('带凭证却被拒 ⇒ rejected，并把闸门自己那句原因透传出来', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({ code: 'UNAUTHORIZED_2001', message: 'bridge token 无效' }, 401)
    );
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.probeBridgeCredential(SERVER, TOKEN);
    expect(r.state).toBe('rejected');
    expect(r.serverMessage).toBe('bridge token 无效');
  });

  it('服务端若回显凭证，判词里要抹掉（绝不把用户刚填的串印回屏幕）', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({ message: `token 无效: ${TOKEN}` }, 401)
    );
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.probeBridgeCredential(SERVER, TOKEN);
    expect(r.state).toBe('rejected');
    expect(r.serverMessage).toBe('');
  });

  it('404（旧版服务端没这个口）判成"探测口不存在"，不算凭证失败', async () => {
    const fetchMock = vi.fn(async () => ({ ok: false, status: 404, json: async () => ({}) }));
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.probeBridgeCredential(SERVER, TOKEN);
    expect(r.state).toBe('no_probe_endpoint');
  });

  it('探测请求本身没通 ⇒ unreachable（不等于凭证不对）', async () => {
    const fetchMock = vi.fn(async () => { throw new TypeError('Failed to fetch'); });
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.probeBridgeCredential(SERVER, TOKEN);
    expect(r.state).toBe('unreachable');
    expect(r.detail).toContain('Failed to fetch');
  });
});

describe('可达探测 testConnection / 读体判降级', () => {
  it('HTTP 200 + 体里 data.status=degraded ⇒ degraded=true（200 不自证健康）', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({ code: 50301, message: 'service degraded', data: { status: 'degraded', checks: {} } })
    );
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.testConnection(SERVER);
    expect(r.ok).toBe(true);
    expect(r.degraded).toBe(true);
    expect(r.degradedByServer).toBe(true);
    expect(r.healthStatus).toBe('degraded');
    expect(r.degradedChecks.length).toBeGreaterThan(0);
  });

  // 活实测的那一条：服务端总判写 ok、embedding 那格写 down，两者能同时出现
  // （health.go 只在 HEALTH_EMBEDDING_CRITICAL=true 时才把 embedding 计入 overallOK）。
  it('HTTP 200 + checks.embedding=down ⇒ 点名 embedding 与服务端给的原因，但不替服务端宣布降级', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({
        code: 0,
        message: 'ok',
        data: {
          status: 'ok',
          checks: {
            database: { status: 'ok', error: '' },
            embedding: { status: 'down', error: 'embedding unreachable: dial tcp 127.0.0.1:8208: connect: connection refused' },
            inference: { status: 'up', error: '' },
            redis: { status: 'ok', error: '' },
          },
        },
      })
    );
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.testConnection(SERVER);
    expect(r.ok).toBe(true);
    expect(r.degraded).toBe(true);
    expect(r.degradedChecks).toHaveLength(1);
    expect(r.degradedChecks[0]).toContain('embedding');
    expect(r.degradedChecks[0]).toContain('127.0.0.1:8208');
    expect(r.degradedByServer).toBe(false);
    expect(r.healthStatus).toBe('ok');
  });

  // 反向控制：正常态的词表不能被一刀切成红——服务端在这些格上本来就写非 'ok' 的值。
  it('not_configured / up / alive 都不算降级（否则健康部署天天见红）', async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({
        code: 0,
        data: {
          status: 'ok',
          checks: {
            redis: { status: 'not_configured', error: '' },
            inference: { status: 'up', error: '' },
            database: { status: 'ok', error: '' },
          },
        },
      })
    );
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.testConnection(SERVER);
    expect(r.ok).toBe(true);
    expect(r.degraded).toBe(false);
    expect(r.degradedChecks).toEqual([]);
  });

  it('响应体不是 JSON 时退回"只判可达"，不凭空造红', async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      json: async () => { throw new Error('not json'); },
    }));
    const { exported } = await loadPopup(fetchMock);
    const r = await exported.testConnection(SERVER);
    expect(r.ok).toBe(true);
    expect(r.degraded).toBe(false);
  });
});

describe('banner 判词合成 buildConnectionVerdict / 用户可见文案', () => {
  const reachOk = { ok: true, url: `${SERVER}/health`, status: 200, degraded: false, degradedChecks: [] };

  it('可达 + 凭证可用 ⇒ 成功，并点名下行形态', async () => {
    const { exported } = await loadPopup();
    const v = exported.buildConnectionVerdict(reachOk, { state: 'verified', sseEnabled: true });
    expect(v.kind).toBe('success');
    expect(v.title).toBe('✓ 可达，凭证可用');
    expect(v.body).toContain('SSE 长连接');
  });

  it('可达 + 凭证被拒 ⇒ 红字，且明说"连着却一条也不同步"', async () => {
    const { exported } = await loadPopup();
    const v = exported.buildConnectionVerdict(reachOk, {
      state: 'rejected', status: 401, serverMessage: 'bridge token 无效',
    });
    expect(v.kind).toBe('error');
    expect(v.title).toBe('✗ 凭证被服务端拒绝');
    expect(v.body).toContain('bridge token 无效');
    expect(v.body).toContain('一条也不同步');
  });

  it('可达 + 未填凭证 ⇒ 红字点名"凭证栏是空的"', async () => {
    const { exported } = await loadPopup();
    const v = exported.buildConnectionVerdict(reachOk, { state: 'missing', status: 401 });
    expect(v.kind).toBe('error');
    expect(v.title).toBe('✗ 未填桥接凭证');
    expect(v.body).toContain('凭证栏是空的');
  });

  it('凭证探测没通 ⇒ 黄字"凭证没能校验"，不冒充已验证', async () => {
    const { exported } = await loadPopup();
    for (const state of ['timeout', 'unreachable', 'no_probe_endpoint']) {
      const v = exported.buildConnectionVerdict(reachOk, { state, status: 404 });
      expect(v.kind).toBe('warn');
      expect(v.title).toContain('凭证没能校验');
    }
  });

  it('服务端自报降级 + 凭证可用 ⇒ 黄字带降级点名，标题不许被"凭证可用"盖掉', async () => {
    const { exported } = await loadPopup();
    const reach = {
      ...reachOk,
      degraded: true,
      degradedByServer: true,
      healthStatus: 'degraded',
      degradedChecks: ['database: sql: conn closed'],
    };
    const v = exported.buildConnectionVerdict(reach, { state: 'verified', sseEnabled: false, pollIntervalMs: 1500 });
    expect(v.kind).toBe('warn');
    expect(v.title).toBe('⚠ 服务端可达但已降级');
    expect(v.body).toContain('database');
    expect(v.body).toContain('轮询');
  });

  // 活实测形状：总判 ok + embedding down ⇒ 服务端没宣布降级，扩展的标题也不许越过它。
  it('仅某一格报 down 而服务端总判 ok ⇒ 标题说"依赖实测故障"，并写清没计为降级', async () => {
    const { exported } = await loadPopup();
    const reach = {
      ...reachOk,
      degraded: true,
      degradedByServer: false,
      healthStatus: 'ok',
      degradedChecks: ['embedding: dial tcp 127.0.0.1:8208'],
    };
    const v = exported.buildConnectionVerdict(reach, { state: 'verified', sseEnabled: true });
    expect(v.kind).toBe('warn');
    expect(v.title).toBe('⚠ 可达，有一项依赖实测故障');
    expect(v.body).toContain('127.0.0.1:8208');
    expect(v.body).toContain('总判为「ok」');
    expect(v.body).toContain('兜底话术');
    expect(v.body).not.toContain('已降级');
  });

  // 严重度取最差：链路走不通（凭证被拒）必须盖过"只是慢"（依赖降级）。
  it('依赖降级 + 凭证被拒 ⇒ 整条 banner 是红', async () => {
    const { exported } = await loadPopup();
    const reach = { ...reachOk, degraded: true, degradedChecks: ['database: sql: conn closed'] };
    const v = exported.buildConnectionVerdict(reach, { state: 'rejected', status: 401, serverMessage: 'bridge token 无效' });
    expect(v.kind).toBe('error');
    expect(v.body).toContain('database');
  });

  it('健康口全不通 ⇒ 沿用「✗ 无法连接」与排查清单', async () => {
    const { exported } = await loadPopup();
    const v = exported.buildConnectionVerdict(
      { ok: false, url: SERVER, reason: 'unreachable', detail: 'Failed to fetch' },
      null
    );
    expect(v.kind).toBe('error');
    expect(v.title).toBe('✗ 无法连接');
    expect(v.body).toContain('Failed to fetch');
    expect(v.body).toContain('host_permissions');
  });

  it('showBanner 把判词原样落到 DOM（渲染腿：用户看到的是这条字符串）', async () => {
    const { exported, elements } = await loadPopup();
    const v = exported.buildConnectionVerdict(reachOk, {
      state: 'rejected', status: 401, serverMessage: 'bridge token 无效',
    });
    exported.showBanner(v.kind, v.title, v.body);
    const banner = elements['banner'];
    expect(banner.className).toBe('banner show error');
    expect(banner.textContent).toContain('✗ 凭证被服务端拒绝');
    expect(banner.textContent).toContain('一条也不同步');
  });
});

describe('点击处理取的元素在 index.html 里都存在', () => {
  // 本文件的 DOM 桩会把任何 id 凭空造出来（getElementById 缺项就 new 一个），
  // 所以「按钮读的输入框在 HTML 里根本没有」这种错在用例里永远是绿的。
  // 而它在真 popup 上的症状是点一下就抛 TypeError——正是这颗按钮唯一的失效方式。
  it('src/popup/index.js 里每个 $(id) 都能在 src/popup/index.html 找到', () => {
    const src = readFileSync(resolve(__dirname, '../src/popup/index.js'), 'utf8');
    const html = readFileSync(resolve(__dirname, '../src/popup/index.html'), 'utf8');
    const used = [...new Set([...src.matchAll(/\$\('([A-Za-z][A-Za-z0-9]*)'\)/g)].map((m) => m[1]))];
    const present = new Set([...html.matchAll(/id="([A-Za-z][A-Za-z0-9]*)"/g)].map((m) => m[1]));
    // 正控制：名单本身非空，否则"零命中"会被读成"全对得上"。
    expect(used.length).toBeGreaterThan(20);
    const missing = used.filter((id) => !present.has(id));
    expect(missing).toEqual([]);
  });
});
