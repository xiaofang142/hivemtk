
// @vitest-environment node
//
// popup-online-injection.test.js —— 「构建期注入线上地址」是否真的生效。
//
// 为什么要单开一个文件：2026-11-04 二次审核发现，原实现把注入写成
//   (typeof process !== 'undefined' && process.env && process.env.BRIDGE_ONLINE_SERVER)
// esbuild 只能替换掉 process.env.X 那一小段，产物变成
//   typeof process<"u" && process.env && "https://…" || ""
// MV3 扩展里没有 process 全局 ⇒ 运行时恒短路 ⇒ **带 env 构建出来的包也不预填云端地址**。
// 当时的用例只覆盖「不注入」路径，所以这条静默失效没人抓到。
// 本文件用带 define 的打包，断言注入路径在「浏览器语义」下真的可见。
//
// 两个方向都要守：
//   ① 注入 → 预填线上、按钮可见、地址文案正确；
//   ② 不注入 → 预填本地、线上按钮隐藏（开源/自部署零厂商域名）。
import { describe, it, expect, vi, afterEach } from 'vitest';
import { build } from 'esbuild';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const popupEntry = resolve(__dirname, '../src/popup/index.js');
const ONLINE_URL = 'https://bridge.example.test';

const cache = new Map();

// 按「是否注入」分别打包一次并缓存（define 不同 ⇒ 产物不同）
function bundlePopup(injected) {
  const key = injected ? 'injected' : 'oss';
  if (cache.has(key)) return cache.get(key);
  const p = build({
    entryPoints: [popupEntry],
    bundle: true,
    format: 'iife',
    write: false,
    platform: 'browser',
    target: 'es2020',
    minify: true,
    logLevel: 'silent',
    define: { __BRIDGE_ONLINE_SERVER__: JSON.stringify(injected ? ONLINE_URL : '') },
  }).then((r) => r.outputFiles[0].text);
  cache.set(key, p);
  return p;
}

async function loadPopup(injected) {
  const popupSrc = await bundlePopup(injected);
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
        style: { display: '' },
        classList: { add: () => {}, remove: () => {} },
        _listeners: {},
        addEventListener(type, fn) {
          (this._listeners[type] = this._listeners[type] || []).push(fn);
        },
        click() {
          for (const fn of this._listeners.click || []) fn({ key: '' });
        },
        appendChild(n) {
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
  const docListeners = {};
  const document = {
    getElementById: (id) => mk(id),
    createElement: (tag) => ({ tagName: tag, className: '', textContent: '', appendChild: (n) => n }),
    addEventListener: (type, fn) => {
      (docListeners[type] = docListeners[type] || []).push(fn);
    },
  };
  const window = { __popup: null, addEventListener: () => {} };
  const chrome = {
    storage: { local: { get: (_k, cb) => cb({}), set: (_kv, cb) => cb && cb() } },
    runtime: { lastError: null, sendMessage: (_m, cb) => cb && cb({ ok: true }) },
    tabs: { query: (_q, cb) => cb([]) },
  };
  const ctx = {
    document,
    window,
    chrome,
    AbortController,
    fetch: () => Promise.resolve({ ok: false }),
    setTimeout,
    clearTimeout,
    setInterval,
    clearInterval,
    console,
  };
  const fn = new Function(
    ...Object.keys(ctx),
    `${popupSrc}\nreturn typeof window !== "undefined" ? window.__popup : null;`
  );
  const exported = fn(...Object.values(ctx));
  vi.useFakeTimers();
  try {
    for (const fn of docListeners.DOMContentLoaded || []) fn();
    await vi.runAllTimersAsync();
  } finally {
    vi.useRealTimers();
  }
  return { exported, elements };
}

afterEach(() => {
  vi.useRealTimers();
});

describe('线上地址构建期注入（SaaS 发行包）', () => {
  it('注入后：输入框预填线上地址', async () => {
    const { elements } = await loadPopup(true);
    expect(elements['serverUrl'].value).toBe(ONLINE_URL);
  });

  it('注入后：placeholder 也是线上地址', async () => {
    const { elements } = await loadPopup(true);
    expect(elements['serverUrl'].placeholder).toBe(ONLINE_URL);
  });

  it('注入后：线上按钮可见，且按钮上的地址文案是域名（去 scheme）', async () => {
    const { elements } = await loadPopup(true);
    expect(elements['useOnline'].style.display).not.toBe('none');
    expect(elements['useOnlineAddr'].textContent).toBe('bridge.example.test');
  });

  it('注入后：「本地」按钮仍能切回本机地址', async () => {
    const { elements } = await loadPopup(true);
    elements['useLocal'].click();
    expect(elements['serverUrl'].value).toBe('http://localhost:8204');
  });
});

describe('未注入（开源 / 自部署构建）', () => {
  it('不注入：预填本地地址，绝不预填任何厂商域名', async () => {
    const { elements } = await loadPopup(false);
    expect(elements['serverUrl'].value).toBe('http://localhost:8204');
    expect(elements['serverUrl'].value).not.toContain('example.test');
  });

  it('不注入：线上按钮隐藏（不诱导自部署用户连厂商域）', async () => {
    const { elements } = await loadPopup(false);
    expect(elements['useOnline'].style.display).toBe('none');
  });
});
