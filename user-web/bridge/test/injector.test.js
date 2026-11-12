import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import {
  pickContentScriptFile,
  injectContentScript,
  scriptingAvailable,
  isSupportedHost,
  SUPPORTED_HOSTS,
  __resetInjectState,
} from '../src/background/injector.js';

describe('pickContentScriptFile / URL 路由', () => {
  it('抖音主站 → content-douyin.js', () => {
    expect(pickContentScriptFile('https://www.douyin.com/follow')).toBe('content-douyin.js');
  });

  it('抖音创作者中心（子域）→ content-douyin.js', () => {
    expect(pickContentScriptFile('https://creator.douyin.com/')).toBe('content-douyin.js');
  });

  it('小红书 → content-xhs.js', () => {
    expect(pickContentScriptFile('https://www.xiaohongshu.com/message')).toBe('content-xhs.js');
  });

  it('TikTok → content-tiktok.js', () => {
    expect(pickContentScriptFile('https://www.tiktok.com/messages')).toBe('content-tiktok.js');
  });

  it('非支持域名 → null', () => {
    expect(pickContentScriptFile('https://example.com/foo')).toBe(null);
    expect(pickContentScriptFile('https://baidu.com/')).toBe(null);
  });

  it('无效输入 → null', () => {
    expect(pickContentScriptFile(null)).toBe(null);
    expect(pickContentScriptFile(undefined)).toBe(null);
    expect(pickContentScriptFile('')).toBe(null);
    expect(pickContentScriptFile('not a url')).toBe(null);
  });

  it('大小写不敏感', () => {
    expect(pickContentScriptFile('HTTPS://WWW.DOUYIN.COM/')).toBe('content-douyin.js');
  });

  it('闲鱼 goofish.com → content-xianyu.js', () => {
    expect(pickContentScriptFile('https://www.goofish.com/im')).toBe('content-xianyu.js');
  });

  it('闲鱼 xianyu.com → content-xianyu.js', () => {
    expect(pickContentScriptFile('https://www.xianyu.com/chat')).toBe('content-xianyu.js');
  });

  it('闲鱼子域 m.goofish.com → content-xianyu.js', () => {
    expect(pickContentScriptFile('https://m.goofish.com/im')).toBe('content-xianyu.js');
  });

  it('拼多多商家后台 → content-pdd.js（CH-16 首切片）', () => {
    expect(pickContentScriptFile('https://mms.pinduoduo.com/chat-merchant/index.html')).toBe('content-pdd.js');
    expect(pickContentScriptFile('https://pinduoduo.com/')).toBe('content-pdd.js');
    // 邻近平台不误命中。
    expect(pickContentScriptFile('https://yangkeduo.com/')).toBe(null);
    expect(SUPPORTED_HOSTS).toContain('pinduoduo.com');
  });

  // 2026-11-12 B站注入面扩展：新私信中心 www.bilibili.com/message/ 与 #/whisper 等
  // hash 入口（hash 无法进 manifest matches，只能靠 onCommitted 按 host 路由）。
  // 广注入 + adapter match() 严格门激活——douyin/weibo 同款规范。
  it('B站旧私信中心 message.bilibili.com → content-bilibili.js', () => {
    expect(pickContentScriptFile('https://message.bilibili.com/#/whisper')).toBe('content-bilibili.js');
  });

  it('B站主站 www.bilibili.com → content-bilibili.js（广路由，激活由 match() 决定）', () => {
    expect(pickContentScriptFile('https://www.bilibili.com/message/')).toBe('content-bilibili.js');
    expect(pickContentScriptFile('https://www.bilibili.com/#/whisper')).toBe('content-bilibili.js');
    expect(pickContentScriptFile('https://www.bilibili.com/video/BVxx')).toBe('content-bilibili.js');
    expect(SUPPORTED_HOSTS).toContain('www.bilibili.com');
  });
});

describe('isSupportedHost / 运行时白名单', () => {
  it('SUPPORTED_HOSTS 包含 5 个平台', () => {
    expect(SUPPORTED_HOSTS).toContain('douyin.com');
    expect(SUPPORTED_HOSTS).toContain('xiaohongshu.com');
    expect(SUPPORTED_HOSTS).toContain('tiktok.com');
    expect(SUPPORTED_HOSTS).toContain('goofish.com');
    expect(SUPPORTED_HOSTS).toContain('xianyu.com');
  });

  it('支持域名主站 → true', () => {
    expect(isSupportedHost('https://www.douyin.com/')).toBe(true);
    expect(isSupportedHost('https://www.xiaohongshu.com/')).toBe(true);
    expect(isSupportedHost('https://www.tiktok.com/')).toBe(true);
    expect(isSupportedHost('https://www.goofish.com/')).toBe(true);
    expect(isSupportedHost('https://www.xianyu.com/')).toBe(true);
  });

  it('支持域名子域 → true', () => {
    expect(isSupportedHost('https://creator.douyin.com/')).toBe(true);
    expect(isSupportedHost('https://m.goofish.com/im')).toBe(true);
    expect(isSupportedHost('https://v.douyin.com/abc')).toBe(true);
  });

  it('非支持域名 → false', () => {
    expect(isSupportedHost('https://example.com/')).toBe(false);
    expect(isSupportedHost('https://baidu.com/')).toBe(false);
    expect(isSupportedHost('https://evil-douyin.com/')).toBe(false); 
    expect(isSupportedHost('https://douyin.com.evil.com/')).toBe(false); 
  });

  it('无效输入 → false', () => {
    expect(isSupportedHost(null)).toBe(false);
    expect(isSupportedHost(undefined)).toBe(false);
    expect(isSupportedHost('')).toBe(false);
    expect(isSupportedHost('not a url')).toBe(false);
  });

  it('大小写不敏感', () => {
    expect(isSupportedHost('HTTPS://WWW.DOUYIN.COM/')).toBe(true);
    expect(isSupportedHost('https://WWW.Goofish.COM/im')).toBe(true);
  });
});

describe('scriptingAvailable / 探测', () => {
  it('chrome.scripting.executeScript 存在 → true', () => {
    const saved = globalThis.chrome;
    globalThis.chrome = { scripting: { executeScript: () => {} } };
    expect(scriptingAvailable()).toBe(true);
    globalThis.chrome = saved;
  });

  it('chrome.scripting 缺失 → false', () => {
    const saved = globalThis.chrome;
    globalThis.chrome = { runtime: {} };
    expect(scriptingAvailable()).toBe(false);
    globalThis.chrome = saved;
  });

  it('chrome 完全缺失 → false', () => {
    const saved = globalThis.chrome;
    delete globalThis.chrome;
    expect(scriptingAvailable()).toBe(false);
    globalThis.chrome = saved;
  });
});

describe('injectContentScript / 主流程', () => {
  let executeScriptMock;

  beforeEach(() => {
    executeScriptMock = vi.fn();
    globalThis.chrome = {
      scripting: { executeScript: executeScriptMock },
      runtime: { lastError: null },
    };
    __resetInjectState();
  });

  afterEach(() => {
    delete globalThis.chrome;
  });

  // 通用 mock 工厂：成功路径
  function mockSuccess() {
    executeScriptMock.mockImplementation((opts, cb) => {
      setTimeout(() => cb(), 0);
    });
  }

  // 通用 mock 工厂：lastError 失败路径
  function mockLastError(message) {
    executeScriptMock.mockImplementation((opts, cb) => {
      setTimeout(() => {
        globalThis.chrome.runtime.lastError = { message };
        cb();
      }, 0);
    });
  }

  it('成功路径 → { ok: true, file }', async () => {
    mockSuccess();
    const r = await new Promise((resolve) => {
      injectContentScript(42, 'https://www.douyin.com/follow', resolve);
    });
    expect(r.ok).toBe(true);
    expect(r.file).toBe('content-douyin.js');
    expect(executeScriptMock).toHaveBeenCalledTimes(1);
    const [arg] = executeScriptMock.mock.calls[0];
    expect(arg).toEqual({
      target: { tabId: 42, allFrames: false },
      files: ['content-douyin.js'],
    });
  });

  it('lastError 失败 → { ok: false, reason: execute_failed }', async () => {
    mockLastError('permission denied');
    const r = await new Promise((resolve) => {
      injectContentScript(1, 'https://www.douyin.com/', resolve);
    });
    expect(r.ok).toBe(false);
    expect(r.reason).toBe('execute_failed');
    expect(r.error).toContain('permission denied');
  });

  it('executeScript 抛异常 → { ok: false, reason: execute_threw }', async () => {
    executeScriptMock.mockImplementation(() => {
      throw new Error('boom');
    });
    const r = await new Promise((resolve) => {
      injectContentScript(1, 'https://www.douyin.com/', resolve);
    });
    expect(r.ok).toBe(false);
    expect(r.reason).toBe('execute_threw');
    expect(r.error).toContain('boom');
  });

  it('非支持 host → { ok: false, reason: unsupported_host }，不调用 executeScript', async () => {
    const r = await new Promise((resolve) => {
      injectContentScript(1, 'https://example.com/', resolve);
    });
    expect(r.ok).toBe(false);
    expect(r.reason).toBe('unsupported_host');
    expect(executeScriptMock).not.toHaveBeenCalled();
  });

  it('allFrames=true 透传到 target.allFrames', async () => {
    mockSuccess();
    await new Promise((resolve) => {
      injectContentScript(1, 'https://www.douyin.com/', { allFrames: true }, resolve);
    });
    const [arg] = executeScriptMock.mock.calls[0];
    expect(arg.target).toEqual({ tabId: 1, allFrames: true });
  });

  it('dedup：同 (tabId, file) 1.5s 内重复 → 第二次 dedup=true', async () => {
    mockSuccess();
    const r1 = await new Promise((resolve) => {
      injectContentScript(99, 'https://www.douyin.com/', resolve);
    });
    const r2 = await new Promise((resolve) => {
      injectContentScript(99, 'https://www.douyin.com/', resolve);
    });
    expect(r1.ok).toBe(true);
    expect(r1.dedup).toBeUndefined();
    expect(r2.ok).toBe(true);
    expect(r2.dedup).toBe(true);
    expect(executeScriptMock).toHaveBeenCalledTimes(1);
  });

  it('不同 tabId 各自独立（不被 dedup）', async () => {
    mockSuccess();
    const r1 = await new Promise((resolve) => {
      injectContentScript(1, 'https://www.douyin.com/', resolve);
    });
    const r2 = await new Promise((resolve) => {
      injectContentScript(2, 'https://www.douyin.com/', resolve);
    });
    expect(r1.ok).toBe(true);
    expect(r2.ok).toBe(true);
    expect(r1.dedup).toBeUndefined();
    expect(r2.dedup).toBeUndefined();
    expect(executeScriptMock).toHaveBeenCalledTimes(2);
  });

  it('opts 缺省时 allFrames=false', async () => {
    mockSuccess();
    await new Promise((resolve) => {
      injectContentScript(1, 'https://www.douyin.com/', resolve);
    });
    const [arg] = executeScriptMock.mock.calls[0];
    expect(arg.target.allFrames).toBe(false);
  });

  it('opts 位置传函数（兼容旧调用）→ opts 视为空', async () => {
    mockSuccess();
    await new Promise((resolve) => {
      injectContentScript(1, 'https://www.douyin.com/', resolve);
    });
    expect(executeScriptMock).toHaveBeenCalledTimes(1);
  });
});

