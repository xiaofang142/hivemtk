import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import {
  SENDER,
  typedContentHash,
  contentHash,
  mediaPlaceholder,
  isMediaMsgType,
  MEDIA_PLACEHOLDERS,
} from '../src/core/types.js';
import { buildDouyinAdapter } from '../src/channels/douyin.js';
import { buildXhsAdapter } from '../src/channels/xhs.js';
import { buildXianyuAdapter } from '../src/channels/xianyu.js';
import { buildTiktokAdapter } from '../src/channels/tiktok.js';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

function makeAdapter(hooks = {}) {
  return new BaseAdapter({
    name: 'test',
    channel: 'douyin_web',
    SEL: {},
    hooks: {
      match: () => true,
      getAccountId: () => 'acc1',
      getConversationId: () => 'conv1',
      getMessageItems: () => [],
      ...hooks,
    },
  });
}

// ── types.js：typed 哈希与占位符词表（与服务端 ContentHashMsgIDTyped 同构） ──

describe('002 多类型消息：typedContentHash / 占位符词表', () => {
  it('text / 空类型退化为 contentHash（存量字节不变）', () => {
    expect(typedContentHash('douyin', 'c1', 'text', '', '你好')).toBe(contentHash('douyin', 'c1', '你好'));
    expect(typedContentHash('douyin', 'c1', '', '', '你好')).toBe(contentHash('douyin', 'c1', '你好'));
    expect(typedContentHash('douyin', 'c1', 'TEXT', '', '你好')).toBe(contentHash('douyin', 'c1', '你好'));
  });

  it('同占位符、不同 media_url 的两张图哈希不同（修复点）', () => {
    const a = typedContentHash('douyin', 'c1', 'image', 'https://f/x1.png', '[图片]');
    const b = typedContentHash('douyin', 'c1', 'image', 'https://f/x2.png', '[图片]');
    expect(a).not.toBe(b);
    expect(a).not.toBe(contentHash('douyin', 'c1', '[图片]'));
    // 同图重发哈希稳定（幂等前提）
    expect(a).toBe(typedContentHash('douyin', 'c1', 'image', 'https://f/x1.png', '[图片]'));
  });

  it('mediaPlaceholder 词表覆盖主类型，未知类型给中性占位', () => {
    expect(mediaPlaceholder('image')).toBe('[图片]');
    expect(mediaPlaceholder('video')).toBe('[视频]');
    expect(mediaPlaceholder('voice')).toBe('[语音]');
    expect(mediaPlaceholder('file')).toBe('[文件]');
    expect(mediaPlaceholder('link')).toBe('[链接]');
    expect(mediaPlaceholder('')).toBe('[消息]');
    expect(Object.isFrozen(MEDIA_PLACEHOLDERS)).toBe(true);
  });

  it('isMediaMsgType：text/system/recall 不是媒体', () => {
    expect(isMediaMsgType('image')).toBe(true);
    expect(isMediaMsgType('VIDEO')).toBe(true);
    expect(isMediaMsgType('text')).toBe(false);
    expect(isMediaMsgType('')).toBe(false);
    expect(isMediaMsgType('system')).toBe(false);
    expect(isMediaMsgType('recall')).toBe(false);
  });
});

// ── 上行：媒体消息不再被管线丢弃 ──

describe('002 多类型消息：上行管线不再丢弃媒体消息', () => {
  it('getMessages：图片气泡（无文字）上行 msg_type=image + media_url', () => {
    const adapter = makeAdapter({
      getMessageItems: () => [{}],
      parseMessageItem: () => ({
        sender_type: SENDER.CUSTOMER,
        sender_id: 'buyer1',
        text: '',
        msg_type: 'image',
        media_url: 'https://cdn.x/img1.png',
        timestamp: 1000,
      }),
    });
    const out = adapter.getMessages();
    expect(out).toHaveLength(1);
    expect(out[0].msg_type).toBe('image');
    expect(out[0].media_url).toBe('https://cdn.x/img1.png');
  });

  it('_ingest：媒体消息放行并上行（占位符补齐 content）', () => {
    const adapter = makeAdapter({
      parseMessageItem: () => ({
        sender_type: SENDER.CUSTOMER,
        text: '',
        msg_type: 'video',
        media_url: 'https://cdn.x/v.mp4',
      }),
    });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    adapter._ingest({
      sender_type: SENDER.CUSTOMER,
      text: '',
      msg_type: 'video',
      media_url: 'https://cdn.x/v.mp4',
      timestamp: 1,
    });
    expect(onMessage).toHaveBeenCalledTimes(1);
    const msg = onMessage.mock.calls[0][0];
    expect(msg.msg_type).toBe('video');
    expect(msg.media_url).toBe('https://cdn.x/v.mp4');
    expect(msg.content).toBe('[视频]');
  });

  it('同会话两张图（同占位符、不同 URL）去重键不同，不互吞', () => {
    const adapter = makeAdapter({});
    const k1 = adapter._dedupKey('c1', { sender_id: 'b1', text: '', msg_type: 'image', media_url: 'https://f/1.png' });
    const k2 = adapter._dedupKey('c1', { sender_id: 'b1', text: '', msg_type: 'image', media_url: 'https://f/2.png' });
    expect(k1).not.toBe(k2);
    // 文本消息键保持旧口径（_sentKeys 存量不重算）
    const t1 = adapter._dedupKey('c1', { sender_id: 'b1', text: 'hi', msg_type: 'text' });
    const t2 = adapter._dedupKey('c1', { sender_id: 'b1', text: 'hi' });
    expect(t1).toBe(t2);
  });

  it('_emitPatrolMessage：顶层按最后一条真实类型，不再写死 text', () => {
    const adapter = makeAdapter({});
    const batch = [
      { sender_type: SENDER.CUSTOMER, sender_id: 'b1', text: '看下这个', msg_type: 'text', message_id: 'm1', timestamp: 1 },
      { sender_type: SENDER.CUSTOMER, sender_id: 'b1', text: '', msg_type: 'image', media_url: 'https://f/i.png', message_id: 'm2', timestamp: 2 },
    ];
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    adapter._emitPatrolMessage('c1', null, batch);
    const msg = onMessage.mock.calls[0][0];
    expect(msg.msg_type).toBe('image');
    expect(msg.media_url).toBe('https://f/i.png');
    expect(msg.content).toBe('[图片]');
    // history 每轮带真实类型
    expect(msg.history[1].msg_type).toBe('image');
    expect(msg.history[1].media_url).toBe('https://f/i.png');
    expect(msg.history[1].content).toBe('[图片]');
  });
});

// ── 下行：媒体出站分派与降级 ──

describe('002 多类型消息：sendOutbound 媒体路径', () => {
  it('无 sendMedia hook：图片出站降级为「[图片] URL」文本，成功并 ack', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const adapter = makeAdapter({ sendText });
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', {
      sendVerifyMs: 0,
      msgType: 'image',
      mediaUrl: 'https://files.internal/i.png',
    });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(sendText).toHaveBeenCalledTimes(1);
    const sent = sendText.mock.calls[0][0];
    expect(sent).toContain('[图片]');
    expect(sent).toContain('https://files.internal/i.png');
  });

  it('有 sendMedia hook 且成功：走原生路径，不调 sendText', async () => {
    const sendText = vi.fn();
    const sendMedia = vi.fn().mockResolvedValue({ ok: true });
    const adapter = makeAdapter({ sendText, sendMedia });
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', {
      sendVerifyMs: 0,
      msgType: 'video',
      mediaUrl: 'https://files.internal/v.mp4',
    });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.via).toBe('sendMedia');
    expect(sendMedia).toHaveBeenCalledWith('video', 'https://files.internal/v.mp4', 'conv1', expect.anything());
    expect(sendText).not.toHaveBeenCalled();
  });

  it('sendMedia 失败：降级文本投递，也算投递成功', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const sendMedia = vi.fn().mockResolvedValue({ ok: false });
    const adapter = makeAdapter({ sendText, sendMedia });
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', {
      sendVerifyMs: 0,
      msgType: 'file',
      mediaUrl: 'https://files.internal/doc.pdf',
    });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(sendText).toHaveBeenCalled();
    expect(sendText.mock.calls[0][0]).toContain('https://files.internal/doc.pdf');
  });

  it('自报出站携带真实类型与 URL（回声去重对齐 typed 口径）', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const adapter = makeAdapter({ sendText });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    const p = adapter.sendOutbound('', 'conv1', {
      sendVerifyMs: 0,
      msgType: 'image',
      mediaUrl: 'https://f/i.png',
    });
    await vi.advanceTimersByTimeAsync(4000);
    await p;
    const selfReport = onMessage.mock.calls.find((c) => c[0].sender_type === SENDER.AGENT)?.[0];
    expect(selfReport).toBeTruthy();
    expect(selfReport.msg_type).toBe('image');
    expect(selfReport.media_url).toBe('https://f/i.png');
    expect(selfReport.event_id).toBe(typedContentHash('douyin_web', 'conv1', 'image', 'https://f/i.png', ''));
  });

  it('纯文本路径行为不变（回归）', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const adapter = makeAdapter({ sendText });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    const p = adapter.sendOutbound('hello', 'conv1', { sendVerifyMs: 0 });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(sendText.mock.calls[0][0]).toBe('hello');
    const selfReport = onMessage.mock.calls.find((c) => c[0].sender_type === SENDER.AGENT)?.[0];
    expect(selfReport.msg_type).toBe('text');
    expect(selfReport.event_id).toBe(contentHash('douyin_web', 'conv1', 'hello'));
  });
});

// ── M-4（2026-11-11）：voice/emoji URL 提取（全渠道 media_url 恒空收口）──
// 审计登记：voice/emoji 全渠道标类型但 media_url 恒空，AI 只能收到类型占位符；
// 且 media_url 参与 _dedupKey 与 event_id（typedContentHash），URL 恒空 ⇒ 同会话
// 连续两条语音键全同 ⇒ 第二条起被客户端 _hasSent + 服务端 msg_id 幂等双重吞掉。
// 收口口径：DOM 有可提取 src 一并提取，提取不到保持类型标注（AI 侧已按类型拼上下文）。

describe('M-4 voice/emoji URL 提取（主力 4 渠道）', () => {
  beforeEach(() => { document.body.innerHTML = ''; });

  const dyItem = (html) => {
    document.body.innerHTML = `<div class="msg-list">${html}</div>`;
    return document.querySelector('.msg-list > div');
  };

  it('douyin：纯语音 audio[src] → msg_type=voice + media_url（此前恒空）', () => {
    const a = buildDouyinAdapter();
    const p = a.parseMessageItem(dyItem('<div class="bubble"><div class="voice-msg"><audio src="https://p3.douyinvod.com/v.m4a"></audio></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('https://p3.douyinvod.com/v.m4a');
  });

  it('douyin：语音容器 data-src 兜底提取（无 audio 元素形态）', () => {
    const a = buildDouyinAdapter();
    const p = a.parseMessageItem(dyItem('<div class="bubble"><div class="voice-msg" data-src="//p3.douyinvod.com/x.m4a"></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('http://p3.douyinvod.com/x.m4a');
  });

  it('douyin：语音提取不到 URL → 保持 voice 类型标注（media_url 空，不丢消息）', () => {
    const a = buildDouyinAdapter();
    const p = a.parseMessageItem(dyItem('<div class="bubble"><div class="voice-msg"></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('');
  });

  it('douyin：表情容器 data-src → emoji + media_url；font-icon 无 src → 仅类型标注', () => {
    const a = buildDouyinAdapter();
    const withUrl = a.parseMessageItem(dyItem('<div class="bubble"><span class="emoji emoji-1" data-src="//sf1-ttcdn-tos.pstatp.com/e.png"></span></div>'));
    expect(withUrl.msg_type).toBe('emoji');
    expect(withUrl.media_url).toBe('http://sf1-ttcdn-tos.pstatp.com/e.png');
    const fontIcon = a.parseMessageItem(dyItem('<div class="bubble"><span class="emoji emoji-smile"></span></div>'));
    expect(fontIcon).not.toBeNull();
    expect(fontIcon.msg_type).toBe('emoji');
    expect(fontIcon.media_url).toBe('');
  });

  it('douyin：两条不同 URL 的语音 dedupKey/event_id 互不碰撞（此前恒空键全同互吞）', () => {
    const a = buildDouyinAdapter();
    const p1 = a.parseMessageItem(dyItem('<div class="bubble"><div class="voice-msg"><audio src="https://p3.douyinvod.com/1.m4a"></audio></div></div>'));
    const p2 = a.parseMessageItem(dyItem('<div class="bubble"><div class="voice-msg"><audio src="https://p3.douyinvod.com/2.m4a"></audio></div></div>'));
    expect(a._dedupKey('conv1', p1)).not.toBe(a._dedupKey('conv1', p2));
    expect(a._canonicalMsgId(dyItem('<div class="bubble"><div class="voice-msg"><audio src="https://p3.douyinvod.com/1.m4a"></audio></div></div>'), 'conv1', p1, 0))
      .not.toBe(a._canonicalMsgId(dyItem('<div class="bubble"><div class="voice-msg"><audio src="https://p3.douyinvod.com/2.m4a"></audio></div></div>'), 'conv1', p2, 0));
    // 同 URL 重渲染仍幂等（去重语义不破坏）
    const p1b = a.parseMessageItem(dyItem('<div class="bubble"><div class="voice-msg"><audio src="https://p3.douyinvod.com/1.m4a"></audio></div></div>'));
    expect(a._dedupKey('conv1', p1)).toBe(a._dedupKey('conv1', p1b));
  });

  it('xhs：data-msg-type=VOICE + audio[data-src] → voice + media_url', () => {
    const a = buildXhsAdapter();
    document.body.innerHTML = '<div class="im-msg-item" data-msg-type="VOICE"><div class="left"><audio data-src="https://sig.xhscdn.com/v.m4a"></audio></div></div>';
    const p = a.parseMessageItem(document.querySelector('.im-msg-item'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('https://sig.xhscdn.com/v.m4a');
  });

  it('xianyu：voice 容器 audio[src] → voice + media_url', () => {
    const a = buildXianyuAdapter();
    document.body.innerHTML = '<div class="msg-item"><div class="voice"><audio src="https://xianyu.im/v.m4a"></audio></div></div>';
    const p = a.parseMessageItem(document.querySelector('.msg-item'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('https://xianyu.im/v.m4a');
  });

  it('tiktok：voice 容器 audio[src] → voice + media_url', () => {
    const a = buildTiktokAdapter();
    document.body.innerHTML = '<div class="msg-item"><div class="voice"><audio src="https://tiktok.im/v.m4a"></audio></div></div>';
    const p = a.parseMessageItem(document.querySelector('.msg-item'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('https://tiktok.im/v.m4a');
  });
});
