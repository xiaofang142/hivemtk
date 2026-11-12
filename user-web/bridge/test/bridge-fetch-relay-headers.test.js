// 2026-11-13 生产下行死锁回归：relay 成功返回后 bridgeFetch 仍直连的根因。
// 背景 SW 的 bridgeFetch handler 回 { status, statusText, headers, body }，
// headers 是 Object.fromEntries() 的普通对象；content 侧旧实现
// `new Map(res.headers || [])` 对普通对象必抛 TypeError（不可迭代），
// 落入 catch 的直连回退 → 生产 workapi CORS 判死刑 → 幽灵认领 pa+1。
// 本文件锁三件事：
//   1. 对象形状 headers（生产真实形状）能构造出可用 Map；
//   2. 数组形状 headers（老版本契约）同样兼容；
//   3. 页面上下文 relay 失败时绝不回退直连（幽灵认领唯一来源），错误原样抛。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { bridgeFetch } from '../src/core/http-ingest.js';

const PAGE_URL = 'https://workapi.xapptool.cn/api/bridge/outbox?channel=douyin';

describe('bridgeFetch 页面上下文 relay 契约', () => {
  let sendMessageMock;
  let fetchMock;

  beforeEach(() => {
    sendMessageMock = vi.fn();
    fetchMock = vi.fn(async () => {
      throw new Error('页面源直连绝不许发生（CORS 判死刑 + 幽灵认领）');
    });
    globalThis.chrome = {
      runtime: { id: 'test-ext-id', sendMessage: sendMessageMock },
    };
    globalThis.fetch = fetchMock;
  });

  afterEach(() => {
    delete globalThis.chrome;
    delete globalThis.fetch;
  });

  it('relay 成功 + 对象 headers（生产形状）→ 状态/体/头全透传', async () => {
    sendMessageMock.mockResolvedValue({
      status: 200,
      statusText: 'OK',
      headers: { 'content-type': 'application/json', 'x-tenant': 'tz3rmdj' },
      body: '{"status":"ok","messages":[{"msg_id":"mh:abc"}]}',
    });
    const res = await bridgeFetch(PAGE_URL, { method: 'GET' });
    expect(res.ok).toBe(true);
    expect(res.status).toBe(200);
    expect(res.headers.get('content-type')).toBe('application/json');
    expect(res.headers.get('x-tenant')).toBe('tz3rmdj');
    expect(await res.json()).toEqual({
      status: 'ok',
      messages: [{ msg_id: 'mh:abc' }],
    });
    expect(await res.text()).toBe(
      '{"status":"ok","messages":[{"msg_id":"mh:abc"}]}'
    );
    expect(sendMessageMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('relay 成功 + 数组 headers（老契约）→ 同样兼容', async () => {
    sendMessageMock.mockResolvedValue({
      status: 204,
      statusText: 'No Content',
      headers: [['retry-after', '1']],
      body: '',
    });
    const res = await bridgeFetch(PAGE_URL, { method: 'GET' });
    expect(res.ok).toBe(true);
    expect(res.headers.get('retry-after')).toBe('1');
    expect(await res.json()).toBe(null);
  });

  it('relay reject → 原样抛出，绝不回退页面源直连', async () => {
    sendMessageMock.mockRejectedValue(new Error('SW not awake'));
    await expect(bridgeFetch(PAGE_URL, { method: 'GET' })).rejects.toThrow(
      'SW not awake'
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('relay 回 __err → 包装抛出，绝不回退页面源直连', async () => {
    sendMessageMock.mockResolvedValue({ __err: 'SW 500' });
    await expect(bridgeFetch(PAGE_URL, { method: 'GET' })).rejects.toThrow(
      'SW 500'
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
