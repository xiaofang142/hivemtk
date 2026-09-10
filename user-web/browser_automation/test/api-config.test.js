import { describe, it, expect, beforeEach, vi } from 'vitest';
import { normalizeServerUrl, DEFAULT_USER_SERVER, STEP_ACTIONS } from '../src/core/constants.js';

describe('constants', () => {
  it('默认地址是 user-server 统一端口 8204', () => {
    expect(DEFAULT_USER_SERVER.baseUrl).toBe('http://localhost:8204');
    expect(DEFAULT_USER_SERVER.healthPaths).toContain('/healthz');
  });

  it('normalizeServerUrl 补协议去尾斜杠', () => {
    expect(normalizeServerUrl('localhost:8204')).toBe('http://localhost:8204');
    expect(normalizeServerUrl('http://127.0.0.1:8204/')).toBe('http://127.0.0.1:8204');
    expect(normalizeServerUrl('  https://api.x.cn// ')).toBe('https://api.x.cn');
    expect(normalizeServerUrl('')).toBe('');
  });

  it('原语下拉与后端 dto oneof 数量一致', () => {
    expect(STEP_ACTIONS.length).toBe(11);
    expect(STEP_ACTIONS.map((a) => a.value)).toContain('wait_for_selector');
  });
});

describe('api-client 配置存取（chrome.storage.local mock）', () => {
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

  it('未配置时返回空（由调用方落到默认地址）', async () => {
    const { loadConfig: lc } = await import('../src/core/api-client.js');
    const cfg = await lc();
    expect(cfg).toEqual({ serverUrl: '' });
  });

  it('saveConfig 持久化 + getConfiguredBaseUrl 落到默认', async () => {
    const { saveConfig: sc, getConfiguredBaseUrl: gb } = await import('../src/core/api-client.js');
    await sc({ serverUrl: 'http://192.168.1.5:8204' });
    expect(store.browserAutomationConfig.serverUrl).toBe('http://192.168.1.5:8204');
    expect(await gb()).toBe('http://192.168.1.5:8204');

    await sc({ serverUrl: '' });
    expect(await gb()).toBe('http://localhost:8204');
  });

  it('token 剩余 <1h 时 apiCall 自动静默续期（密码永不落盘）', async () => {
    const expSoon = Math.floor(Date.now() / 1000) + 1800; // 30 分钟后过期
    const makeJWT = (exp) => 'h.' + Buffer.from(JSON.stringify({ exp })).toString('base64') + '.s';
    store.browserAutomationAuth = { token: makeJWT(expSoon), exp: expSoon, username: 'u1', serverUrl: 'http://localhost:8204' };

    const newExp = Math.floor(Date.now() / 1000) + 86400;
    global.fetch = vi.fn(async () => ({
      status: 200,
      json: async () => ({ code: 0, data: { token: makeJWT(newExp) } }),
    }));
    const { listTasks } = await import('../src/core/api-client.js');
    await listTasks('limit=1');
    expect(store.browserAutomationAuth.exp).toBe(newExp);
    expect(store.browserAutomationAuth.password).toBeUndefined();
  });

  it('token 充裕时不触发 refresh', async () => {
    const expFar = Math.floor(Date.now() / 1000) + 86400;
    const makeJWT = (exp) => 'h.' + Buffer.from(JSON.stringify({ exp })).toString('base64') + '.s';
    store.browserAutomationAuth = { token: makeJWT(expFar), exp: expFar, username: 'u1', serverUrl: 'http://localhost:8204' };

    global.fetch = vi.fn(async () => ({
      status: 200,
      json: async () => ({ code: 0, data: { list: [], total: 0 } }),
    }));
    const { listTasks } = await import('../src/core/api-client.js');
    await listTasks('limit=1');
    expect(global.fetch).toHaveBeenCalledTimes(1); // 只有业务调用，无 refresh
  });
});
