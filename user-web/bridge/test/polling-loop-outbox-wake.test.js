import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const downlink = vi.hoisted(() => ({ poll: vi.fn(async () => {}), init: vi.fn(), sse: vi.fn() }));
vi.mock('../src/core/downlink.js', () => ({ pollDownlink: downlink.poll, initDownlink: downlink.init, startSSEDelivery: downlink.sse }));
import { PollingLoop } from '../src/core/polling-loop.js';

let listener;
let loop;
let account;
let config;
let messages;
beforeEach(() => {
  vi.useFakeTimers();
  downlink.poll.mockReset().mockResolvedValue(undefined);
  downlink.sse.mockReset();
  account = 'account-a';
  config = { serverUrl: 'https://service.example', token: 'wake-test-secret', tenantCode: 'tenant-a' };
  messages = [];
  vi.stubGlobal('chrome', {
    runtime: {
      id: 'extension-id',
      onMessage: { addListener: (fn) => { listener = fn; }, removeListener: vi.fn() },
      sendMessage: vi.fn(async (message) => { messages.push(message); return { registered: true }; }),
    },
  });
  loop = new PollingLoop({ channels: ['douyin'], pageContext: true, getConfig: async () => config, getMeta: () => ({ accountId: account }), getAdapter: () => null });
});
afterEach(() => { loop?.stop(); vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals(); listener = undefined; });

async function start() { await loop.start(); await vi.advanceTimersByTimeAsync(0); }
async function wake(overrides = {}, sender = { id: 'extension-id' }) {
  const registration = messages.findLast((message) => message.type === 'BRIDGE_WAKE_REGISTER');
  expect(registration).toBeDefined();
  expect(JSON.stringify(registration)).not.toContain('wake-test-secret');
  listener({ ...registration, type: 'BRIDGE_OUTBOX_READY', ...overrides }, sender);
  await vi.advanceTimersByTimeAsync(0);
}

describe('唤醒只补拉原轮询', () => {
  it('固定通知走原pollDownlink；1500毫秒兜底仍在且不启第二SSE发送器', async () => {
    await start();
    await wake();
    expect(downlink.poll).toHaveBeenCalledTimes(1);
    expect(downlink.poll.mock.calls[0].slice(0, 2)).toEqual(['douyin', 'account-a']);
    expect(downlink.sse).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1500);
    expect(downlink.poll).toHaveBeenCalledTimes(2);
  });

  it('发送忙时多次通知只合并一次，不并行；停止后不继续补拉', async () => {
    let release;
    downlink.poll.mockImplementationOnce(() => new Promise((resolve) => { release = resolve; }));
    await start();
    await wake();
    await wake();
    await wake();
    expect(downlink.poll).toHaveBeenCalledTimes(1);
    release();
    await vi.advanceTimersByTimeAsync(0);
    expect(downlink.poll).toHaveBeenCalledTimes(2);
    loop.stop();
    await wake();
    await vi.advanceTimersByTimeAsync(30000);
    expect(downlink.poll).toHaveBeenCalledTimes(2);
  });

  it('拒绝旧账号、旧配置、外部sender与伪注册；重新登记只传身份', async () => {
    await start();
    await wake({ registrationId: 'foreign-registration' });
    await wake({}, { id: 'foreign-extension' });
    account = 'account-b';
    await wake();
    account = 'account-a';
    config = { ...config, token: 'changed-secret' };
    await wake();
    expect(downlink.poll).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(30000);
    expect(messages.filter((message) => message.type === 'BRIDGE_WAKE_REGISTER').length).toBeGreaterThan(1);
    expect(JSON.stringify(messages)).not.toContain('changed-secret');
    downlink.poll.mockClear();
    await wake();
    expect(downlink.poll).toHaveBeenCalledTimes(1);
  });

  it('忙时旧账号通知在完成前变更，不能作为新账号补拉理由', async () => {
    let release;
    downlink.poll.mockImplementationOnce(() => new Promise((resolve) => { release = resolve; }));
    await start();
    await wake();
    await wake();
    account = 'account-b';
    release();
    await vi.advanceTimersByTimeAsync(0);
    expect(downlink.poll).toHaveBeenCalledTimes(1);
  });
});
