// SSE 建连被拒时的取证契约（2026-09-28）
//
// 起因：connectSSE 原来写 `throw new Error('SSE 连接失败: HTTP ' + status)`，把响应体整块丢掉。
// 服务端闸门在这条路由上把原因写在体里（401 分「缺少 X-Bridge-Token」与「bridge token 无效」
// 两种，400 分「缺 channel/account_id」与「渠道不在白名单」），运维在扩展日志里只看到一个
// 状态码数字 ⇒ 与"扩展已连接却一条也不同步"同源的第二次现场：现象在手、原因不在手。
// 同时重连侧要能区分"网络断了"（该退避重试）与"请求本身是坏的"（重打无意义），
// 所以错误对象必须带 nonRetryable——沿用 http-ingest.js 的同名字段口径。
import { describe, it, expect, vi, afterEach } from 'vitest';

import { connectSSE } from '../src/core/sse-fetch-client.js';

const TOKEN = 'bridge-secret-xyz';
const SERVER = 'http://localhost:8204';

function stubResponse(body, status) {
  globalThis.fetch = vi.fn(async () => new Response(body, { status }));
}

// 取 connectSSE 抛出的错误：它同时会经 onError 回调外抛，但判据走 reject 这条更直白。
async function sseError(body, status) {
  stubResponse(body, status);
  const err = await connectSSE('douyin', 'acc-1', {
    serverUrl: SERVER,
    token: TOKEN,
    onMessage: () => {},
  }).catch((e) => e);
  expect(err).toBeInstanceOf(Error);
  // 凭证绝不进错误信息：这条串会被 content script 打到页面控制台。
  expect(err.message).not.toContain(TOKEN);
  return err;
}

afterEach(() => delete globalThis.fetch);

describe('4xx 错误体透传', () => {
  it('401 缺凭证：服务端的 message 进错误信息，不只剩状态码', async () => {
    const err = await sseError('{"code":"UNAUTHORIZED_2001","message":"缺少 X-Bridge-Token"}', 401);
    expect(err.message).toContain('缺少 X-Bridge-Token');
    expect(err.message).toContain('401');
    expect(err.status).toBe(401);
    expect(err.code).toBe('UNAUTHORIZED_2001');
    expect(err.serverMessage).toBe('缺少 X-Bridge-Token');
  });

  it('401 凭证无效与缺凭证可区分（两者 message 不同）', async () => {
    const bad = await sseError('{"code":"UNAUTHORIZED_2001","message":"bridge token 无效"}', 401);
    expect(bad.serverMessage).toBe('bridge token 无效');
    expect(bad.message).not.toBe('SSE 连接失败: HTTP 401');
  });

  it('400 缺参：闸门回的原因原样可见', async () => {
    const err = await sseError('{"code":"INVALID_PARAM_2004","message":"缺少参数 account_id"}', 400);
    expect(err.message).toContain('缺少参数 account_id');
    expect(err.nonRetryable).toBe(true);
  });

  it('非 JSON 错误体（网关 HTML）不炸取证，用原文兜底', async () => {
    const err = await sseError('<html>502 Bad Gateway</html>', 400);
    expect(err.code).toBe('');
    expect(err.serverMessage).toBe('');
    expect(err.message).toContain('502 Bad Gateway');
  });

  it('超长错误体截到前缀，不把日志撑爆', async () => {
    const long = 'x'.repeat(4000);
    const err = await sseError(long, 400);
    expect(err.message.length).toBeLessThan(700);
  });
});

describe('可重试性判定（与 http-ingest.js 同口径）', () => {
  it.each([
    [400, true],
    [401, true],
    [403, true],
    [404, true],
    [408, false],
    [429, false],
    [500, false],
    [503, false],
  ])('HTTP %i → nonRetryable=%s', async (status, wantNonRetryable) => {
    const err = await sseError(`{"message":"st-${status}"}`, status);
    expect(!!err.nonRetryable).toBe(wantNonRetryable);
  });
});
