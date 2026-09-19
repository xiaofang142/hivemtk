// 批3 工程卫生：B4 超时预算 / B5 stop 全清 / B6 跨上下文限流闸 + 静态契约。
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, it, expect, afterEach } from 'vitest';
import { humanSendTimeoutMs, BRIDGE_THREE_CHANNEL } from '../src/core/constants.js';
import { globalSendWaitMs, stampGlobalSendAt, readGlobalLastSendAt, GLOBAL_SEND_AT_KEY } from '../src/core/rate-limiter.js';
import { buildDouyinAdapter } from '../src/channels/douyin.js';

const readSrc = (rel) => readFileSync(join(process.cwd(), 'src', rel), 'utf8');

describe('B4 humanSendTimeoutMs（长度感知超时预算）', () => {
  it('空文本=基础值；线性 +250ms/码点；封顶 120s；emoji 按码点计', () => {
    const base = BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs;
    expect(humanSendTimeoutMs('')).toBe(base);
    expect(humanSendTimeoutMs('你好👋ok')).toBe(base + 5 * 250); // Array.from 码点数（emoji=1）
    expect(humanSendTimeoutMs('x'.repeat(1000))).toBe(120_000);
    expect(humanSendTimeoutMs('abc', 1000)).toBe(1000 + 3 * 250);
  });
  it('静态契约：发送全链三处均走长度感知超时，无裸 await', () => {
    const ca = readSrc('core/channel-adapter.js');
    expect(ca).toContain('withTimeout(this.rawSendText(text), humanSendTimeoutMs(text)');
    const dl = readSrc('core/downlink.js');
    expect(dl).toContain('sendOutbound-SSE'); // SSE 路径对称超时
    expect(dl.match(/humanSendTimeoutMs\(/g).length).toBeGreaterThanOrEqual(3); // poll + retry + SSE
    expect(dl).not.toContain("console.log('[bridge FULL]");
  });
  it('静态契约：uplink 吞错已接日志并回插 buffer', () => {
    const ul = readSrc('core/uplink.js');
    expect(ul).toContain('uplink flush 失败');
    expect(ul).toContain('restored');
  });
});

describe('B5 stop() 全清 timer（行为）', () => {
  it('stop 后 _mutationTimer 不再触发 _flushMutations，且所有 timer 置 null', async () => {
    const a = buildDouyinAdapter();
    let flushed = 0;
    a._flushMutations = () => { flushed++; };
    a._onMutations([{ addedNodes: [] }]); // 挂一个 100ms 的 _mutationTimer
    expect(a._mutationTimer).toBeTruthy();
    a.stop();
    expect(a._mutationTimer).toBe(null);
    expect(a._pendingMutations).toEqual([]);
    await new Promise((r) => setTimeout(r, 160));
    expect(flushed).toBe(0);
    a.stop(); // 幂等：二次 stop 不抛
  });
  it('静态契约：activate 监听器自我摘除；SSE stop 走 key 级 abort + 唤醒退避', () => {
    const common = readSrc('content/common.js');
    expect(common).toContain("window.removeEventListener('beforeunload', cleanup)");
    expect(common).toContain("window.removeEventListener('pagehide', cleanup)");
    const dl = readSrc('core/downlink.js');
    expect(dl).toContain('stopSSE(channel, accountId)');
    expect(dl).toContain('wakeWait()');
    const sc = readSrc('core/sse-fetch-client.js');
    expect(sc).toContain('export function stopSSE');
  });
});

describe('B6 跨上下文全局发送闸', () => {
  const savedChrome = globalThis.chrome;
  afterEach(() => {
    if (savedChrome === undefined) delete globalThis.chrome;
    else globalThis.chrome = savedChrome;
  });

  it('无 chrome.storage → 0ms 降级（不阻塞发送）', async () => {
    globalThis.chrome = undefined;
    expect(await globalSendWaitMs(5000)).toBe(0);
    expect(await readGlobalLastSendAt()).toBe(0);
    await stampGlobalSendAt(123); // 不抛
  });

  it('stamp 后按剩余间隔返回等待；旧时间戳返回 0', async () => {
    const store = {};
    globalThis.chrome = {
      storage: {
        local: {
          get: async (k) => ({ [k]: store[k] }),
          set: async (o) => { Object.assign(store, o); },
        },
      },
    };
    await stampGlobalSendAt(Date.now() - 100); // 100ms 前全局发过
    const wait = await globalSendWaitMs(2000);
    expect(wait).toBeGreaterThan(1700);
    expect(wait).toBeLessThanOrEqual(2000);
    await stampGlobalSendAt(Date.now() - 60000);
    expect(await globalSendWaitMs(2000)).toBe(0);
  });

  it('未来时间戳（时钟漂移）判无效防自锁；minInterval<=0 直接 0', async () => {
    const store = { [GLOBAL_SEND_AT_KEY]: Date.now() + 3_600_000 };
    globalThis.chrome = { storage: { local: { get: async (k) => ({ [k]: store[k] }), set: async () => {} } } };
    expect(await globalSendWaitMs(5000)).toBe(0);
    globalThis.chrome = { storage: { local: { get: async () => ({ 'mtk_global_last_send_at': 'not-a-number' }), set: async () => {} } } };
    expect(await readGlobalLastSendAt()).toBe(0);
    await stampGlobalSendAt();
  });

  it('静态契约：fillAndSend 在 rawSendText 前过闸、成功后盖章', () => {
    const ca = readSrc('core/channel-adapter.js');
    const start = ca.indexOf('async sendOutbound(text');
    const body = ca.slice(start, start + 3000);
    expect(body.length).toBeGreaterThan(100); // 方法体确实存在
    expect(body.indexOf('globalSendWaitMs')).toBeGreaterThan(-1);
    expect(body.indexOf('globalSendWaitMs')).toBeLessThan(body.indexOf('rawSendText(text)'));
    expect(body).toContain('stampGlobalSendAt()');
  });
});
