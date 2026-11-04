
// @vitest-environment node
//
// popup-server-url-default.test.js —— 「服务端地址：两个提示（本地/线上）+ 默认线上」行为测试。
// 与 popup.test.js 同一套路：esbuild 打包 popup 入口 → new Function 注入 mock DOM，
// 但这里要**触发 DOMContentLoaded** 才能覆盖初始化路径（placeholder / 预填值 / 快捷按钮），
// 因此 mock 元素带事件捕获，chrome.storage 可编程返回「已保存配置」或「空配置」。
// 强制 node 环境的理由同 popup.test.js（jsdom 跨 realm Uint8Array 不变量会炸 esbuild）。
import { describe, it, expect, vi, afterEach } from 'vitest';
import { build } from 'esbuild';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const popupEntry = resolve(__dirname, '../src/popup/index.js');

// 线上/本地地址预期值 —— 与 src/core/constants.js 单源对齐（常量本身已有 constants.test.js 守门），
// 这里只断言 popup 的接线行为是否把两个地址用对了位置。
const ONLINE_URL = 'https://workapi.xapptool.cn';
const LOCAL_URL = 'http://localhost:8204';

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

// 构造可触发初始化的 mock 环境。
// storageCfg: chrome.storage.local.get('bridgeConfig') 回的内容（null = 空配置）。
async function loadPopupInteractive(storageCfg) {
  const popupSrc = await bundlePopup();
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
        addEventListener: function (type, fn) {
          (this._listeners[type] = this._listeners[type] || []).push(fn);
        },
        click: function () {
          for (const fn of this._listeners.click || []) fn({ key: '' });
        },
        keydown: function (e) {
          for (const fn of this._listeners.keydown || []) fn(e);
        },
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
  const docListeners = {};
  const document = {
    getElementById: (id) => mk(id),
    createElement: (tag) => mkChild(tag),
    addEventListener: (type, fn) => {
      (docListeners[type] = docListeners[type] || []).push(fn);
    },
  };
  const winListeners = {};
  const window = { __popup: null, addEventListener: (type, fn) => { (winListeners[type] = winListeners[type] || []).push(fn); } };
  const storage = {
    local: {
      get: (_key, cb) => cb(storageCfg === undefined ? {} : { bridgeConfig: storageCfg }),
      set: (_kv, cb) => cb && cb(),
    },
  };
  const ctx = {
    document,
    window,
    chrome: {
      storage,
      runtime: { lastError: null, sendMessage: (_msg, cb) => cb && cb({ ok: true }) },
      tabs: { query: (_q, cb) => cb([]) },
    },
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
  // 触发初始化（与真实 popup 加载一致）；告警轮询里的 setInterval 用假计时器兜住。
  vi.useFakeTimers();
  try {
    for (const fn of docListeners.DOMContentLoaded || []) fn();
    await vi.runAllTimersAsync();
  } finally {
    vi.useRealTimers();
  }
  return { exported, elements, window, document };
}

afterEach(() => {
  vi.useRealTimers();
});

describe('popup 服务端地址默认值（两个提示：本地/线上，默认线上）', () => {
  it('placeholder 是线上地址（默认推荐）', async () => {
    const { elements } = await loadPopupInteractive(undefined);
    expect(elements['serverUrl'].placeholder).toBe(ONLINE_URL);
  });

  it('无已存配置时，输入框预填线上地址（开箱即连云端）', async () => {
    const { elements } = await loadPopupInteractive(undefined);
    expect(elements['serverUrl'].value).toBe(ONLINE_URL);
  });

  it('已有已存配置时，显示已存值，不被线上地址覆盖', async () => {
    const { elements } = await loadPopupInteractive({ serverUrl: 'http://192.168.1.10:8204', token: 'tkn' });
    expect(elements['serverUrl'].value).toBe('http://192.168.1.10:8204');
  });

  it('「线上」快捷按钮一键填入线上地址', async () => {
    const { elements } = await loadPopupInteractive(undefined);
    elements['serverUrl'].value = 'something-else';
    elements['useOnline'].click();
    expect(elements['serverUrl'].value).toBe(ONLINE_URL);
  });

  it('「本地」快捷按钮一键填入本地地址（自部署场景）', async () => {
    const { elements } = await loadPopupInteractive(undefined);
    elements['useLocal'].click();
    expect(elements['serverUrl'].value).toBe(LOCAL_URL);
  });

  it('两个快捷按钮填的地址互不相同，且与输入框初始默认（线上）一致', async () => {
    const { elements } = await loadPopupInteractive(undefined);
    const initial = elements['serverUrl'].value;
    elements['useOnline'].click();
    expect(elements['serverUrl'].value).toBe(initial);
    elements['useLocal'].click();
    expect(elements['serverUrl'].value).not.toBe(initial);
    expect(elements['serverUrl'].value).toBe(LOCAL_URL);
  });
});
