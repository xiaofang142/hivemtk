import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const boundary = vi.hoisted(() => ({ enqueue: vi.fn(), getOutbox: vi.fn(), ackOutbox: vi.fn() }));

// 仅替换网络/轮询边界；实际执行 downlink 成功记账和 content 上报回调。
vi.mock('../src/core/http-ingest.js', () => ({
  getOutbox: boundary.getOutbox,
  ackOutbox: boundary.ackOutbox,
}));
vi.mock('../src/core/uplink.js', () => ({
  Uplink: class { enqueue(message) { boundary.enqueue(message); } },
}));
vi.mock('../src/core/polling-loop.js', () => ({
  PollingLoop: class { start() {} stop() {} },
  getServerCapabilities: async () => ({}),
}));
vi.mock('../src/core/selector-ai.js', () => ({
  hydrateSelectors: vi.fn(), SELECTOR_UPDATE_MSG: 'selector-update',
}));
vi.mock('../src/core/logger.js', () => ({
  createLogger: () => ({ info() {}, warn() {}, error() {}, debug() {} }),
}));

describe('content 客户同文消息上报边界', () => {
  let storage;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-11-15T12:00:00Z'));
    vi.resetModules();
    vi.clearAllMocks();
    delete window.__bridgeStarted;
    delete window.__bridgeAdapter;
    storage = { bridgeConfig: { serverUrl: 'http://localhost:8204', token: 'test-token' } };
    vi.stubGlobal('chrome', {
      runtime: { onMessage: { addListener: vi.fn() } },
      storage: { local: {
        get: (keys, callback) => {
          const result = Object.fromEntries((Array.isArray(keys) ? keys : [keys])
            .filter((key) => key in storage).map((key) => [key, storage[key]]));
          if (callback) callback(result);
          return Promise.resolve(result);
        },
        set: async (value) => { Object.assign(storage, value); },
      } },
    });
    boundary.ackOutbox.mockResolvedValue({ status: 'ok' });
  });

  afterEach(() => {
    window.dispatchEvent(new Event('pagehide'));
    delete window.__bridgeStarted;
    delete window.__bridgeAdapter;
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  async function sendThenCollect({ channel = 'douyin', account = 'account-a', conversation = 'conv-a', content = '你好' } = {}) {
    const { pollDownlink } = await import('../src/core/downlink.js');
    const { startBridge } = await import('../src/content/common.js');
    boundary.getOutbox.mockResolvedValue({ status: 'ok', messages: [{
      msg_id: 'outbound-1', conversation_id: 'conv-a', content: '你好',
    }] });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('douyin', 'account-a', async () => storage.bridgeConfig, { sendOutbound });
    expect(sendOutbound).toHaveBeenCalledTimes(1);
    expect(boundary.ackOutbox).toHaveBeenCalledTimes(1);

    let onMessage;
    startBridge(channel, () => ({
      match: () => true,
      getAccountId: () => account,
      start: (callbacks) => { onMessage = callbacks.onMessage; },
      stop() {},
      setPatrolUnreadFirstChannels() {},
      setFollowUserEnabled() {},
    }));
    const message = {
      event_id: 'customer-independent-1', sender_type: 'customer', sender_id: 'customer-b',
      conversation_id: conversation, content,
    };
    onMessage(message);
    return { message, account };
  }

  it.each([
    ['同会话的独立客户回复', {}],
    ['另一会话的客户回复', { conversation: 'conv-b' }],
    ['另一账号的客户回复', { account: 'account-b' }],
    ['仅空白不同的客户回复', { content: '你 好' }],
    ['其他渠道的客户回复', { channel: 'xhs' }],
  ])('%s 不因刚发送相同文本而丢失', async (_scenario, options) => {
    const { message, account } = await sendThenCollect(options);
    expect(boundary.enqueue).toHaveBeenCalledExactlyOnceWith({ ...message, account_id: account });
  });

  it('不同文本继续正常上报', async () => {
    const { message, account } = await sendThenCollect({ content: '请问怎么下单？' });
    expect(boundary.enqueue).toHaveBeenCalledExactlyOnceWith({ ...message, account_id: account });
  });
});
