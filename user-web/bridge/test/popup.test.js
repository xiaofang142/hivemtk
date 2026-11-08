
// @vitest-environment node
//
// popup.test.js 自建 mock DOM（new Function 注入），不依赖 jsdom 全局。
// 强制 node 环境：jsdom 的 TextEncoder/Uint8Array 跨 realm 会让 esbuild 的
// `encode("") instanceof Uint8Array` 不变量校验失败（Invariant violation）。
import { describe, it, expect, vi } from 'vitest';
import { build } from 'esbuild';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const popupEntry = resolve(__dirname, '../src/popup/index.js');

// 用 esbuild 一次性把 popup（含 constants.js）打成单文件 IIFE 并缓存。
// 后续 loadPopup 复用同一份产物，避免每个用例重复打包拖慢测试。
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

async function loadPopup(globals = {}) {
  const popupSrc = await bundlePopup();
  // 构造 jsdom 风格的 window/document
  const elements = {};
  const mk = (id) => {
    if (!elements[id]) {
      elements[id] = {
        id,
        className: '',
        textContent: '',
        innerHTML: '',
        value: '',
        placeholder: '',
        disabled: false,
        children: [],
        addEventListener: () => {},
        appendChild: function (n) {
          this.children.push(n);
          if (n && typeof n.textContent === 'string') {
            this.textContent = (this.textContent || '') + n.textContent;
          }
          return n;
        },
        focus: () => {},
        dispatchEvent: () => {},
      };
    }
    return elements[id];
  };
  const mkChild = (tag) => ({
    tagName: tag,
    className: '',
    textContent: '',
    appendChild: (n) => n,
  });
  const document = {
    getElementById: (id) => mk(id),
    createElement: (tag) => mkChild(tag),
    addEventListener: () => {},
  };
  const window = { __popup: null };
  const ctx = {
    document,
    window,
    chrome: globals.chrome || {},
    AbortController: globals.AbortController || AbortController,
    fetch: globals.fetch || (() => Promise.resolve({ ok: false })),
    setTimeout,
    clearTimeout,
    console,
  };
  // 把打包后的 IIFE 注入到 new Function 执行；用对象 keys 顺序传 ctx 保证顺序稳定
  const fn = new Function(
    ...Object.keys(ctx),
    `${popupSrc}\nreturn typeof window !== "undefined" ? window.__popup : null;`
  );
  const exported = fn(...Object.values(ctx));
  return { exported, elements, window, document };
}

describe('popup normalizeServerUrl', () => {
  it('空字符串返回空', async () => {
    const { exported } = await loadPopup({});
    expect(exported.normalizeServerUrl('')).toBe('');
    expect(exported.normalizeServerUrl(null)).toBe('');
    expect(exported.normalizeServerUrl(undefined)).toBe('');
  });

  it('去除首尾空格', async () => {
    const { exported } = await loadPopup({});
    expect(exported.normalizeServerUrl('  http://x:8204  ')).toBe('http://x:8204');
  });

  it('自动补 http://', async () => {
    const { exported } = await loadPopup({});
    expect(exported.normalizeServerUrl('localhost:8204')).toBe('http://localhost:8204');
    expect(exported.normalizeServerUrl('192.168.1.1:8204/path')).toBe('http://192.168.1.1:8204/path');
  });

  it('保留 https', async () => {
    const { exported } = await loadPopup({});
    expect(exported.normalizeServerUrl('https://example.com:8204')).toBe('https://example.com:8204');
  });

  it('去除尾斜杠', async () => {
    const { exported } = await loadPopup({});
    expect(exported.normalizeServerUrl('http://x:8204/')).toBe('http://x:8204');
    expect(exported.normalizeServerUrl('http://x:8204///')).toBe('http://x:8204');
    expect(exported.normalizeServerUrl('http://x:8204/path/')).toBe('http://x:8204/path');
  });
});

describe('popup testConnection', () => {
  it('空 URL 返回 empty', async () => {
    const { exported } = await loadPopup({});
    const r = await exported.testConnection('');
    expect(r.ok).toBe(false);
    expect(r.reason).toBe('empty');
  });

  it('2xx 返回 ok=true', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    const { exported } = await loadPopup({ fetch: fetchMock });
    const r = await exported.testConnection('http://localhost:8204');
    expect(r.ok).toBe(true);
    expect(r.degraded).toBe(false);
    expect(r.status).toBe(200);
    expect(r.url).toBe('http://localhost:8204/health');
  });

  it('503 返回 ok=true degraded=true', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 503 });
    const { exported } = await loadPopup({ fetch: fetchMock });
    const r = await exported.testConnection('http://localhost:8204');
    expect(r.ok).toBe(true);
    expect(r.degraded).toBe(true);
    expect(r.status).toBe(503);
  });

  it('404 跳过当前路径，尝试下一个', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 404 })
      .mockResolvedValueOnce({ ok: true, status: 200 });
    const { exported } = await loadPopup({ fetch: fetchMock });
    const r = await exported.testConnection('http://localhost:8204');
    expect(r.ok).toBe(true);
    expect(r.url).toBe('http://localhost:8204/healthz');
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('401/403 立即返回 http_xxx', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 401 });
    const { exported } = await loadPopup({ fetch: fetchMock });
    const r = await exported.testConnection('http://localhost:8204');
    expect(r.ok).toBe(false);
    expect(r.reason).toBe('http_401');
    expect(r.status).toBe(401);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('网络错误：所有路径都失败时返回 unreachable', async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));
    const { exported } = await loadPopup({ fetch: fetchMock });
    const r = await exported.testConnection('http://localhost:9999');
    expect(r.ok).toBe(false);
    expect(r.reason).toBe('unreachable');
    expect(r.detail).toContain('Failed to fetch');
    expect(fetchMock).toHaveBeenCalledTimes(4); 
  });

  it('第一个失败第二个成功', async () => {
    const fetchMock = vi.fn()
      .mockRejectedValueOnce(new Error('ECONNREFUSED'))
      .mockResolvedValueOnce({ ok: true, status: 200 });
    const { exported } = await loadPopup({ fetch: fetchMock });
    const r = await exported.testConnection('http://localhost:8204');
    expect(r.ok).toBe(true);
    expect(r.url).toBe('http://localhost:8204/healthz');
  });
});

describe('popup showBanner', () => {
  it('设置 className 与内容', async () => {
    const { exported, elements } = await loadPopup({});
    exported.showBanner('success', '标题', '内容');
    const el = elements['banner'];
    expect(el.className).toContain('show');
    expect(el.className).toContain('success');
    expect(el.children.length).toBe(2);
    expect(el.children[0].className).toBe('title');
    expect(el.children[0].textContent).toBe('标题');
    expect(el.children[1].textContent).toBe('内容');
  });

  it('不同 kind 切换 className', async () => {
    const { exported, elements } = await loadPopup({});
    exported.showBanner('error', 'X', 'Y');
    expect(elements['banner'].className).toContain('error');
    exported.showBanner('warn', 'X', 'Y');
    expect(elements['banner'].className).toContain('warn');
    exported.showBanner('info', 'X', 'Y');
    expect(elements['banner'].className).toContain('info');
  });

  it('clearBanner 重置 className', async () => {
    const { exported, elements } = await loadPopup({});
    exported.showBanner('error', 'X', 'Y');
    exported.clearBanner();
    expect(elements['banner'].className).toBe('banner');
  });
});


describe('popup lastError', () => {
  it('无 lastError 时返回 null', async () => {
    const { exported } = await loadPopup({ chrome: { runtime: { lastError: null } } });
    expect(exported.lastError()).toBeNull();
  });

  it('有 lastError.message 时返回字符串（不解包对象）', async () => {
    const { exported } = await loadPopup({
      chrome: { runtime: { lastError: { message: 'Could not establish connection. Receiving end does not exist.' } } },
    });
    expect(exported.lastError()).toBe('Could not establish connection. Receiving end does not exist.');
  });

  it('lastError.message 为 undefined 时返回 "(无错误详情)" 兜底，避免 popup 显示 "undefined"', async () => {
    const { exported } = await loadPopup({
      chrome: { runtime: { lastError: { message: undefined } } },
    });
    const r = exported.lastError();
    expect(r).toBe('(无错误详情)');
    expect(r).not.toBeUndefined();
  });

  it('chrome.runtime 抛错时返回 null', async () => {
    const { exported } = await loadPopup({
      chrome: { runtime: { get lastError() { throw new Error('forbidden'); } } },
    });
    expect(exported.lastError()).toBeNull();
  });
});

describe('popup diagnoseUninjected', () => {
  it('典型 Chrome 错误 + 抖音域名 → 提示扩展禁用/未注入/旧标签页/iframe', async () => {
    const { exported } = await loadPopup({});
    const hint = exported.diagnoseUninjected('Could not establish connection. Receiving end does not exist.', 'https://www.douyin.com/');
    expect(hint).toContain('扩展未加载');
    expect(hint).toContain('Ctrl+Shift+R');
    expect(hint).toContain('iframe');
  });

  it('典型 Chrome 错误 + 非支持域名 → 提示 URL 不在 manifest', async () => {
    const { exported } = await loadPopup({});
    const hint = exported.diagnoseUninjected('Could not establish connection. Receiving end does not exist.', 'https://example.com/');
    expect(hint).toContain('manifest matches');
    expect(hint).not.toContain('iframe');
  });

  it('非典型错误 → 走通用兜底文案', async () => {
    const { exported } = await loadPopup({});
    const hint = exported.diagnoseUninjected('Some weird error', 'https://www.douyin.com/');
    expect(hint).toContain('扩展未加载');
    expect(hint).toContain('content script 尚未执行');
  });

  it('errMsg 为 null 时仍走通用兜底', async () => {
    const { exported } = await loadPopup({});
    const hint = exported.diagnoseUninjected(null, 'https://www.douyin.com/');
    expect(hint).toContain('扩展未加载');
  });
});

describe('popup selfCheck 流程', () => {
  // 工具：构造一个 fake chrome API
  function makeChrome({ tab, pingResp, selfcheckResp, pingError, selfcheckError, deepResp, deepError }) {
    const calls = [];
    const fake = {
      calls,
      tabs: {
        query: (_q, cb) => cb([tab]),
        sendMessage: (id, msg, cb) => {
          calls.push({ id, msg });
          if (msg.type === 'ping') {
            if (pingError) {
              setTimeout(() => {
                Object.defineProperty(fake.runtime, 'lastError', { value: pingError, configurable: true });
                cb(undefined);
                Object.defineProperty(fake.runtime, 'lastError', { value: null, configurable: true });
              }, 0);
            } else {
              cb(pingResp);
            }
          } else if (msg.type === 'selfcheck') {
            if (selfcheckError) {
              setTimeout(() => {
                Object.defineProperty(fake.runtime, 'lastError', { value: selfcheckError, configurable: true });
                cb(undefined);
                Object.defineProperty(fake.runtime, 'lastError', { value: null, configurable: true });
              }, 0);
            } else {
              cb(selfcheckResp);
            }
          } else if (msg.type === 'deepSelfcheck') {
            if (deepError) {
              setTimeout(() => {
                Object.defineProperty(fake.runtime, 'lastError', { value: deepError, configurable: true });
                cb(undefined);
                Object.defineProperty(fake.runtime, 'lastError', { value: null, configurable: true });
              }, 0);
            } else {
              cb(deepResp);
            }
          } else {
            cb(undefined);
          }
        },
      },
      runtime: { lastError: null },
    };
    return fake;
  }

  // 工具：等 setTimeout 0 触发完成
  const flush = () => new Promise((r) => setTimeout(r, 10));

  it('A. 域名不在白名单：直接提示非支持域名，不发任何消息', async () => {
    const tab = { id: 1, url: 'https://example.com/' };
    const chrome = makeChrome({ tab });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    expect(chrome.calls).toEqual([]);
    expect(elements['selfOut'].textContent).toContain('example.com');
    expect(elements['selfOut'].textContent).toContain('不是已支持平台的网页');
  });

  it('A. ping 失败（典型 Chrome 错误）→ 输出诊断文案，不再显示 "undefined"', async () => {
    const tab = { id: 1, url: 'https://www.douyin.com/' };
    const chrome = makeChrome({
      tab,
      pingError: { message: 'Could not establish connection. Receiving end does not exist.' },
    });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    await flush();
    expect(elements['selfOut'].textContent).not.toContain('undefined');
    expect(elements['selfOut'].textContent).toContain('未注入桥接');
    expect(elements['selfOut'].textContent).toContain('Could not establish connection');
    expect(elements['selfOut'].textContent).toContain('Ctrl+Shift+R');
  });

  it('A. ping 失败但 lastError.message 为 undefined（极端 MV3 场景）→ 兜底为 "(无错误详情)"', async () => {
    const tab = { id: 1, url: 'https://www.douyin.com/' };
    const chrome = makeChrome({
      tab,
      pingError: { message: undefined },
    });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    await flush();
    expect(elements['selfOut'].textContent).not.toContain('undefined');
    expect(elements['selfOut'].textContent).toContain('(无错误详情)');
  });

  it('B. ping 成功 + selfcheck matched=false → 自动降级到 deepSelfcheck 展示 DOM 快照', async () => {
    const tab = { id: 1, url: 'https://www.douyin.com/' };
    const chrome = makeChrome({
      tab,
      pingResp: { pong: true, channel: 'douyin_web', matched: false },
      selfcheckResp: { channel: 'douyin_web', matched: false, hint: '请打开私信页' },
      deepResp: {
        ok: true,
        url: 'https://www.douyin.com/',
        hostname: 'www.douyin.com',
        title: '抖音',
        inputCount: 2,
        inputSample: [
          { tag: 'div', selectorHint: 'div[contenteditable="true"]', placeholder: '', text: '请输入消息' },
        ],
        sendBtnCount: 1,
        sendBtnSample: [{ tag: 'button', selectorHint: 'button[aria-label*="发送"]' }],
        listRootCount: 0,
        listRootSample: [],
        accountHints: [{ text: '我的主页', href: 'https://www.douyin.com/user/MS4wLjABAAAAxx' }],
        recommendedSelector: 'div[contenteditable="true"]',
      },
    });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    await flush();
    const out = elements['selfOut'].textContent;
    expect(out).toContain('adapter.match() 返回 false');
    expect(out).toContain('深度自检');
    expect(out).toContain('div[contenteditable="true"]');
    expect(out).toContain('推荐选择器');
    // 验证消息顺序：ping → selfcheck → deepSelfcheck
    const types = chrome.calls.map((c) => c.msg.type);
    expect(types).toEqual(['ping', 'selfcheck', 'deepSelfcheck']);
  });

  it('B. selfcheck matched=false 但 deepSelfcheck 失败（port closed 场景）→ 展示 selfcheck 原文 + 重负载引导', async () => {
    const tab = { id: 1, url: 'https://www.douyin.com/' };
    const chrome = makeChrome({
      tab,
      pingResp: { pong: true, channel: 'douyin_web', matched: false },
      selfcheckResp: { channel: 'douyin_web', matched: false, hint: '请打开私信页' },
      deepError: { message: 'The message port closed before a response was received.' },
    });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    await flush();
    const out = elements['selfOut'].textContent;
    expect(out).toContain('adapter.match() 返回 false');
    expect(out).toContain('deepSelfcheck 失败');
    expect(out).toContain('DOM 极重');
    expect(out).toContain('标签页处于后台');
    expect(out).toContain('Ctrl+Shift+R');
  });

  it('B. selfcheck matched=false 但 deepSelfcheck 失败（content script 未注入场景）→ 给 manifest/扩展禁用引导', async () => {
    const tab = { id: 1, url: 'https://www.douyin.com/' };
    const chrome = makeChrome({
      tab,
      pingResp: { pong: true, channel: 'douyin_web', matched: false },
      selfcheckResp: { channel: 'douyin_web', matched: false, hint: '请打开私信页' },
      deepError: { message: 'Could not establish connection. Receiving end does not exist.' },
    });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    await flush();
    const out = elements['selfOut'].textContent;
    expect(out).toContain('adapter.match() 返回 false');
    expect(out).toContain('deepSelfcheck 失败');
    expect(out).toContain('content script 未注入');
    expect(out).not.toContain('DOM 极重');
  });

  it('C. ping 成功 + selfcheck matched=true → 显示完整桥接数据（accountId/conversationId 不再为空）', async () => {
    const tab = { id: 1, url: 'https://www.douyin.com/follow' };
    const chrome = makeChrome({
      tab,
      pingResp: { pong: true, channel: 'douyin_web', matched: true },
      selfcheckResp: {
        channel: 'douyin_web',
        matched: true,
        accountId: 'A1',
        conversationId: 'C1',
        msgItemCount: 3,
        selectors: { CHAT_LIST: '#island_b69f5' },
        sample: [{ sender: 'customer', text: '你好' }, { sender: 'self', text: '在的' }],
      },
    });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    await flush();
    expect(elements['selfOut'].textContent).toContain('频道: douyin_web');
    expect(elements['selfOut'].textContent).toContain('匹配: true');
    expect(elements['selfOut'].textContent).toContain('账号: A1');
    expect(elements['selfOut'].textContent).toContain('会话: C1');
    expect(elements['selfOut'].textContent).toContain('消息条目: 3');
    expect(elements['selfOut'].textContent).toContain('island_b69f5');
  });

  it('C. selfcheck matched=true + matchMode=fallback → 提示用户「严格选择器已失效」', async () => {
    const tab = { id: 1, url: 'https://www.douyin.com/im/chat/' };
    const chrome = makeChrome({
      tab,
      pingResp: { pong: true, channel: 'douyin_web', matched: true, matchMode: 'fallback' },
      selfcheckResp: {
        channel: 'douyin_web',
        matched: true,
        matchMode: 'fallback',
        accountId: 'A1',
        conversationId: 'C1',
        msgItemCount: 0,
        selectors: { EDITOR: 'div[contenteditable="true"][role="textbox"]' },
        sample: [],
      },
    });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    await flush();
    const out = elements['selfOut'].textContent;
    expect(out).toContain('匹配模式: ⚠ fallback');
    expect(out).toContain('严格选择器已失效');
    expect(out).toContain('把【深度自检】输出发到 issue');
  });

  it('C. selfcheck matched=true + matchMode=strict → 显示「严格选择器命中」', async () => {
    const tab = { id: 1, url: 'https://www.douyin.com/im/chat/' };
    const chrome = makeChrome({
      tab,
      pingResp: { pong: true, channel: 'douyin_web', matched: true, matchMode: 'strict' },
      selfcheckResp: {
        channel: 'douyin_web',
        matched: true,
        matchMode: 'strict',
        accountId: 'A1',
        conversationId: 'C1',
        msgItemCount: 0,
        selectors: {},
        sample: [],
      },
    });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    await flush();
    const out = elements['selfOut'].textContent;
    expect(out).toContain('匹配模式: ✓ strict');
    expect(out).toContain('严格选择器命中');
  });

  it('无活动标签页：直接提示', async () => {
    const chrome = { tabs: { query: (_q, cb) => cb([]) }, runtime: { lastError: null } };
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    expect(elements['selfOut'].textContent).toBe('无活动标签页');
  });

  it('tab.url 不可读（如 chrome:// 页）→ 提示打开非 chrome:// 页面', async () => {
    const tab = { id: 1, url: 'chrome://settings/' };
    const chrome = makeChrome({ tab });
    const { exported, elements } = await loadPopup({ chrome });
    exported.selfCheck();
    expect(elements['selfOut'].textContent).toContain('chrome://');
  });
});

describe('popup formatDeepSnapshot', () => {
  it('完整快照：所有字段都渲染', async () => {
    const { exported } = await loadPopup({});
    const out = exported.formatDeepSnapshot({
      url: 'https://www.douyin.com/follow',
      hostname: 'www.douyin.com',
      title: '抖音 - 我的关注',
      inputCount: 2,
      inputSample: [
        { tag: 'div', selectorHint: 'div[contenteditable="true"]', placeholder: '', text: '' },
        { tag: 'textarea', selectorHint: 'textarea[placeholder*="留言"]', placeholder: '留言', text: '' },
      ],
      sendBtnCount: 1,
      sendBtnSample: [{ tag: 'button', selectorHint: 'button[aria-label*="发送"]' }],
      listRootCount: 1,
      listRootSample: [{ tag: 'div', selectorHint: '[class*="message-list"]' }],
      accountHints: [{ text: '我的', href: 'https://www.douyin.com/user/MS4wLjABAAAAxx' }],
      recommendedSelector: 'div[contenteditable="true"]',
    });
    expect(out).toContain('=== 深度自检');
    expect(out).toContain('URL: https://www.douyin.com/follow');
    expect(out).toContain('域名: www.douyin.com');
    expect(out).toContain('【输入框】共 2 个');
    expect(out).toContain('div[contenteditable="true"]');
    expect(out).toContain('textarea[placeholder*="留言"]');
    expect(out).toContain('placeholder="留言"');
    expect(out).toContain('【发送按钮】共 1 个');
    expect(out).toContain('button[aria-label*="发送"]');
    expect(out).toContain('【消息列表根】共 1 个');
    expect(out).toContain('[class*="message-list"]');
    expect(out).toContain('【账号线索】');
    expect(out).toContain('MS4wLjABAAAAxx');
    expect(out).toContain('【推荐选择器】div[contenteditable="true"]');
  });

  it('空快照：所有 count=0，渲染兜底', async () => {
    const { exported } = await loadPopup({});
    const out = exported.formatDeepSnapshot({
      url: 'https://www.douyin.com/',
      hostname: 'www.douyin.com',
      title: '抖音',
      inputCount: 0,
      inputSample: [],
      sendBtnCount: 0,
      sendBtnSample: [],
      listRootCount: 0,
      listRootSample: [],
      accountHints: [],
      recommendedSelector: null,
    });
    expect(out).toContain('【输入框】共 0 个');
    expect(out).toContain('【发送按钮】共 0 个');
    expect(out).toContain('【消息列表根】共 0 个');
    expect(out).toContain('【推荐选择器】未找到');
    expect(out).toContain('可能不是私信/消息页');
  });

  it('inputSample 中 placeholder 截断到 50 字符', async () => {
    const { exported } = await loadPopup({});
    const longPH = 'x'.repeat(80);
    const out = exported.formatDeepSnapshot({
      url: 'https://x.com/',
      hostname: 'x.com',
      title: 'X',
      inputCount: 1,
      inputSample: [{ tag: 'textarea', selectorHint: 'textarea', placeholder: longPH, text: '' }],
      sendBtnCount: 0,
      sendBtnSample: [],
      listRootCount: 0,
      listRootSample: [],
      accountHints: [],
      recommendedSelector: 'textarea',
    });
    expect(out).toContain('placeholder="' + 'x'.repeat(50));
    expect(out).not.toContain('x'.repeat(51));
  });

  it('accountHints 文本空时显示 "(无文本)"', async () => {
    const { exported } = await loadPopup({});
    const out = exported.formatDeepSnapshot({
      url: 'https://x.com/',
      hostname: 'x.com',
      title: 'X',
      inputCount: 0,
      inputSample: [],
      sendBtnCount: 0,
      sendBtnSample: [],
      listRootCount: 0,
      listRootSample: [],
      accountHints: [{ text: '', href: 'https://x.com/user/abc' }],
      recommendedSelector: null,
    });
    expect(out).toContain('(无文本)');
    expect(out).toContain('https://x.com/user/abc');
  });
});

