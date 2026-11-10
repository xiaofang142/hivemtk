// M-3 下行媒体原生发送测试（2026-11-11）
// 覆盖：sendMedia 分派契约（assertConv/abortToken 透传）/ 降级 via 标记（no-hook /
// sendmedia-failed / sendmedia-error）/ 失败回退不丢行 / 弃用令牌防双发 /
// 风控对 sendMedia 同样生效（同一 tryAcquire 闸 + markSent 记账）/
// media-inject 注入助手（下载两级兜底、file-input/paste 注入、DOM 落地校验、
// hook 工厂清理与 abort 语义）/ background bridge:fetchMedia 代理。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { BaseAdapter } from '../src/core/channel-adapter.js';
import { RateLimiter } from '../src/core/rate-limiter.js';
import { contentHash } from '../src/core/types.js';
import {
  fetchMediaFile,
  mediaBytesToFile,
  mediaFileName,
  mediaMimeFromUrl,
  acceptAllows,
  findUploadInput,
  injectViaFileInput,
  injectViaPaste,
  countMediaNodes,
  waitForMediaInEditor,
  injectMediaToEditor,
  makeSendMediaHook,
} from '../src/core/media-inject.js';

// ── 假 DataTransfer / 环境（jsdom 无 DataTransfer/ClipboardEvent）──
class FakeDataTransfer {
  constructor() {
    this._files = [];
    this.items = { add: (f) => { this._files.push(f); } };
  }
  get files() { return this._files; }
}

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

// ── A. adapter 分派与降级 via 标记 ────────────────────────────────────────

describe('M-3 sendOutbound：sendMedia 分派契约与 via 标记', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('sendMedia 成功：via=sendMedia + mediaMethod 回传，assertConv/abortToken 透传，不调 sendText', async () => {
    const sendText = vi.fn();
    const sendMedia = vi.fn(async (_type, _url, _conv, opts) => {
      expect(typeof opts.assertConv).toBe('function');
      expect(opts.abortToken && typeof opts.abortToken.aborted === 'boolean').toBe(true);
      return { ok: true, method: 'paste' };
    });
    const adapter = makeAdapter({ sendText, sendMedia });
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'image', mediaUrl: 'https://f/i.png' });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.via).toBe('sendMedia');
    expect(ok.mediaMethod).toBe('paste');
    expect(sendMedia).toHaveBeenCalledTimes(1);
    expect(sendText).not.toHaveBeenCalled();
  });

  it('未实现 sendMedia hook：via=text-fallback:no-hook，降级文本投递成功（不丢行）', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const adapter = makeAdapter({ sendText }); // hooks 无 sendMedia
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'image', mediaUrl: 'https://f/i.png' });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.via).toBe('text-fallback:no-hook');
    expect(sendText.mock.calls[0][0]).toContain('[图片]');
    expect(sendText.mock.calls[0][0]).toContain('https://f/i.png');
    // 自报口径与降级文本一致（typed hash），服务端回声去重对齐
    const selfReport = onMessage.mock.calls.find((c) => c[0].sender_type === 'agent')?.[0]
      || onMessage.mock.calls.find((c) => c[0].msg_type === 'image')?.[0];
    expect(selfReport.msg_type).toBe('image');
    expect(selfReport.media_url).toBe('https://f/i.png');
  });

  it('sendMedia 返回 {ok:false}：via=text-fallback:sendmedia-failed，降级文本成功', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const sendMedia = vi.fn().mockResolvedValue({ ok: false, reason: 'inject-not-landed' });
    const adapter = makeAdapter({ sendText, sendMedia });
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'video', mediaUrl: 'https://f/v.mp4' });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.via).toBe('text-fallback:sendmedia-failed');
    expect(sendText).toHaveBeenCalledTimes(1);
    expect(sendText.mock.calls[0][0]).toContain('[视频]');
  });

  it('sendMedia 抛错：via=text-fallback:sendmedia-error，降级文本成功（不丢行）', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const sendMedia = vi.fn().mockRejectedValue(new Error('媒体下载失败(direct: HTTP 404)'));
    const adapter = makeAdapter({ sendText, sendMedia });
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'file', mediaUrl: 'https://f/doc.pdf' });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.via).toBe('text-fallback:sendmedia-error');
    expect(sendText.mock.calls[0][0]).toContain('https://f/doc.pdf');
  });

  it('sendMedia 超时被放弃：abortToken 弃用令牌置位，在途 hook 自检退出，降级文本成功（防双发）', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    // 在途 hook：每 500ms 轮询 abortToken，命中即自检退出（模拟被 withTimeout 放弃后仍在跑）
    const sendMedia = vi.fn((_type, _url, _conv, opts) => new Promise((resolve) => {
      const iv = setInterval(() => {
        if (opts.abortToken.aborted) {
          clearInterval(iv);
          resolve({ ok: false, reason: 'hook-self-aborted' });
        }
      }, 500);
    }));
    const adapter = makeAdapter({ sendText, sendMedia });
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'image', mediaUrl: 'https://f/i.png' });
    await vi.advanceTimersByTimeAsync(26000); // 越过 20s sendMedia 预算
    const ok = await p;
    expect(ok.ok).toBe(true);
    // 超时按异常口径降级（mediaErr = withTimeout reject）
    expect(ok.via).toBe('text-fallback:sendmedia-error');
    // 关键：弃用令牌已置位，hook 自检退出返回 ok:false（不会再点发送 → 无双发）
    expect(sendMedia).toHaveBeenCalledTimes(1);
    expect(sendText).toHaveBeenCalledTimes(1);
    expect(sendText.mock.calls[0][0]).toContain('[图片]');
  });

  it('纯文本出站结果不带 via（既有形状回归）', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const adapter = makeAdapter({ sendText });
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('hello', 'conv1', { sendVerifyMs: 0 });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(ok.via).toBeUndefined();
    expect(sendText.mock.calls[0][0]).toBe('hello');
  });

  it('降级文本自报仍按 typed 口径（event_id 与 typed hash 对齐回归）', async () => {
    const sendText = vi.fn().mockResolvedValue(undefined);
    const adapter = makeAdapter({ sendText });
    const onMessage = vi.fn();
    adapter.start({ onMessage });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'image', mediaUrl: 'https://f/i2.png' });
    await vi.advanceTimersByTimeAsync(4000);
    await p;
    const selfReport = onMessage.mock.calls.find((c) => c[0].msg_type === 'image')?.[0];
    expect(selfReport).toBeTruthy();
    expect(selfReport.event_id).toBeTruthy();
    expect(selfReport.content).toContain('[图片]');
  });
});

// ── B. 风控接入：sendMedia 与纯文本共用同一闸 ─────────────────────────────

describe('M-3 风控：sendMedia 受 rateLimiter 同一闸约束', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('会话冷却期内：rateLimited 拦截，sendMedia 不被分派', async () => {
    const sendText = vi.fn();
    const sendMedia = vi.fn();
    const adapter = makeAdapter({ sendText, sendMedia });
    // 默认 conversationCooldownMs=3000：先 markSent 记账一次，紧随的媒体下发应被冷却拦截
    adapter.rateLimiter.markSent('douyin_web', 'acc1', 'conv1', 'warm-up');
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'image', mediaUrl: 'https://f/i.png' });
    await vi.advanceTimersByTimeAsync(500);
    const ok = await p;
    expect(ok.ok).toBe(false);
    expect(ok.rateLimited).toBe(true);
    expect(sendMedia).not.toHaveBeenCalled();
    expect(sendText).not.toHaveBeenCalled();
  });

  it('分派顺序：rateLimiter.tryAcquire 先于 sendMedia（无绕过通道）', async () => {
    const sendMedia = vi.fn().mockResolvedValue({ ok: true });
    const adapter = makeAdapter({ sendMedia });
    const origTry = adapter.rateLimiter.tryAcquire.bind(adapter.rateLimiter);
    const trySpy = vi.fn(origTry);
    adapter.rateLimiter.tryAcquire = trySpy;
    const origMark = adapter.rateLimiter.markSent.bind(adapter.rateLimiter);
    const markSpy = vi.fn(origMark);
    adapter.rateLimiter.markSent = markSpy;
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'image', mediaUrl: 'https://f/i.png' });
    await vi.advanceTimersByTimeAsync(4000);
    const ok = await p;
    expect(ok.ok).toBe(true);
    expect(trySpy).toHaveBeenCalledTimes(1);
    expect(sendMedia).toHaveBeenCalledTimes(1);
    expect(trySpy.mock.invocationCallOrder[0]).toBeLessThan(sendMedia.mock.invocationCallOrder[0]);
    // 原生成功按 mediaUrl 口径记账（会话冷却对下一条媒体同样生效）
    expect(markSpy).toHaveBeenCalledWith('douyin_web', 'acc1', 'conv1', 'https://f/i.png');
  });

  it('真实 RateLimiter 注入路径：构造参数生效（冷却窗口拦媒体下发）', async () => {
    const sendMedia = vi.fn();
    const adapter = new BaseAdapter({
      name: 'test',
      channel: 'douyin_web',
      SEL: {},
      hooks: {
        match: () => true,
        getAccountId: () => 'acc1',
        getConversationId: () => 'conv1',
        getMessageItems: () => [],
        sendMedia,
      },
      rateLimiter: new RateLimiter({ conversationCooldownMs: 600000 }),
    });
    adapter.rateLimiter.markSent('douyin_web', 'acc1', 'conv1', 'warm-up');
    adapter.start({ onMessage: vi.fn() });
    const p = adapter.sendOutbound('', 'conv1', { sendVerifyMs: 0, msgType: 'image', mediaUrl: 'https://f/i.png' });
    await vi.advanceTimersByTimeAsync(500);
    const ok = await p;
    expect(ok.rateLimited).toBe(true);
    expect(sendMedia).not.toHaveBeenCalled();
  });
});

// ── C. media-inject：下载与注入助手 ───────────────────────────────────────

describe('M-3 media-inject：fetchMediaFile 两级下载兜底', () => {
  afterEach(() => vi.useRealTimers());

  it('content 直取成功：File 组装（名字/类型/字节数），source=direct', async () => {
    const payload = new TextEncoder().encode('hello-image');
    const fetchFn = vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: { get: (k) => (k === 'content-type' ? 'image/png' : k === 'content-length' ? String(payload.byteLength) : null) },
      arrayBuffer: async () => payload.buffer.slice(0),
    }));
    const res = await fetchMediaFile('https://cdn.example.com/a/pic.png', { fetchFn, runtime: null });
    expect(res.source).toBe('direct');
    expect(res.bytes).toBe(payload.byteLength);
    expect(res.mime).toBe('image/png');
    expect(res.file).toBeInstanceOf(File);
    expect(res.file.name).toBe('pic.png');
    expect(res.file.type).toBe('image/png');
    expect(res.file.size).toBe(payload.byteLength);
    expect(fetchFn).toHaveBeenCalledTimes(1);
  });

  it('直取被 CORS 拦（fetch 抛错）→ background 代理兜底：source=proxy', async () => {
    const payload = new TextEncoder().encode('proxy-bytes');
    const b64 = Buffer.from(payload).toString('base64');
    const fetchFn = vi.fn(async () => { throw new TypeError('Failed to fetch'); });
    const runtime = {
      sendMessage: (msg, cb) => {
        expect(msg.type).toBe('bridge:fetchMedia');
        expect(msg.url).toBe('https://cdn.example.com/a/pic.jpg');
        cb({ ok: true, base64: b64, mime: 'image/jpeg', bytes: payload.byteLength });
      },
    };
    const res = await fetchMediaFile('https://cdn.example.com/a/pic.jpg', { fetchFn, runtime });
    expect(res.source).toBe('proxy');
    expect(res.mime).toBe('image/jpeg');
    expect(new TextDecoder().decode(await res.file.arrayBuffer())).toBe('proxy-bytes');
  });

  it('两级全失败：抛「媒体下载失败」（调用方降级文本）', async () => {
    const fetchFn = vi.fn(async () => { throw new TypeError('Failed to fetch'); });
    const runtime = { sendMessage: (_msg, cb) => cb({ ok: false, error: 'HTTP 403' }) };
    await expect(fetchMediaFile('https://cdn.example.com/x.png', { fetchFn, runtime }))
      .rejects.toThrow(/媒体下载失败.*direct:.*proxy: HTTP 403/s);
  });

  it('超限（content-length 超 cap）：直取拒收 → 代理也拒 → 抛 too_large', async () => {
    const fetchFn = vi.fn(async () => ({
      ok: true,
      headers: { get: (k) => (k === 'content-length' ? '999999999' : null) },
      arrayBuffer: async () => new ArrayBuffer(0),
    }));
    const runtime = { sendMessage: (_msg, cb) => cb({ ok: false, error: 'too_large' }) };
    await expect(fetchMediaFile('https://cdn.example.com/big.mp4', { fetchFn, runtime, maxBytes: 1024 }))
      .rejects.toThrow(/too_large|媒体下载失败/s);
  });

  it('非 http(s) URL：直接拒绝（不发请求）', async () => {
    const fetchFn = vi.fn();
    await expect(fetchMediaFile('ftp://x/y.png', { fetchFn, runtime: null })).rejects.toThrow(/media url 非法/);
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it('URL 缺 Content-Type：按扩展名推断 MIME（mediaBytesToFile 兜底）', () => {
    const f = mediaBytesToFile(new Uint8Array([1, 2, 3]), '', 'https://cdn.example.com/a/b/clip.webm');
    expect(f.type).toBe('video/webm');
    expect(f.name).toBe('clip.webm');
  });

  it('mediaFileName / mediaMimeFromUrl：清洗 CDN 长路径与签名 query', () => {
    expect(mediaMimeFromUrl('https://x.com/a/b/c.PNG?x=1')).toBe('image/png');
    expect(mediaMimeFromUrl('https://x.com/a/b/noext')).toBe('');
    expect(mediaFileName('https://x.com/a/%E5%9B%BE.png?token=abc', '')).toBe('图.png');
    expect(mediaFileName('https://x.com/a/b c.png', 'image/png')).toBe('b_c.png');
    const derived = mediaFileName('https://x.com/noext', 'video/quicktime');
    expect(derived).toBe('noext.mov');
  });
});

describe('M-3 media-inject：accept 判定与上传输入定位', () => {
  afterEach(() => { document.body.innerHTML = ''; });

  it('acceptAllows：类型通配 / 扩展名 / 精确 mime / 空 accept 一律不放行', () => {
    expect(acceptAllows('image/*', 'image/png', 'a.png')).toBe(true);
    expect(acceptAllows('image/png,image/jpeg', 'image/jpeg', 'b.jpg')).toBe(true);
    expect(acceptAllows('.png', 'image/png', 'c.png')).toBe(true);
    expect(acceptAllows('video/*', 'image/png', 'd.png')).toBe(false);
    expect(acceptAllows('', 'image/png', 'd.png')).toBe(false);
    expect(acceptAllows(null, 'image/png', 'd.png')).toBe(false);
    expect(acceptAllows('*/*', 'application/pdf', 'e.pdf')).toBe(true);
  });

  it('findUploadInput：编辑器近邻（祖先链）命中 file input；doc 级兜底只认显式 accept', () => {
    document.body.innerHTML = `
      <div id="panel">
        <div id="editor-wrap"><div id="editor" contenteditable="true"></div></div>
        <input type="file" id="chat-upload" accept="image/*,video/*" />
      </div>
      <input type="file" id="avatar-upload" />
      <input type="file" id="doc-upload" accept="application/pdf" />
    `;
    const editor = document.getElementById('editor');
    const hit = findUploadInput(editor, 'image/png', 'a.png', { doc: document });
    expect(hit && hit.id).toBe('chat-upload');
    // doc 级兜底：无 accept 的 avatar-upload 不放行；显式 pdf accept 只放 pdf
    document.getElementById('chat-upload').remove();
    const none = findUploadInput(editor, 'image/png', 'a.png', { doc: document });
    expect(none).toBe(null);
    const pdf = findUploadInput(editor, 'application/pdf', 'doc.pdf', { doc: document });
    expect(pdf && pdf.id).toBe('doc-upload');
  });

  it('findUploadInput：无 accept 的裸 file input 即使在近邻链上也不命中（防误注入头像/无关域）', () => {
    document.body.innerHTML = `
      <div id="panel2">
        <div id="editor2" contenteditable="true"></div>
        <input type="file" id="bare-upload" />
      </div>
    `;
    const editor = document.getElementById('editor2');
    const hit = findUploadInput(editor, 'image/png', 'a.png', { doc: document });
    expect(hit).toBe(null);
  });
});

describe('M-3 media-inject：注入与落地校验', () => {
  afterEach(() => { document.body.innerHTML = ''; });

  it('injectViaFileInput：DataTransfer 塞 files + input/change 派发', () => {
    const file = new File(['x'], 'a.png', { type: 'image/png' });
    const input = { files: null, dispatchEvent: vi.fn() };
    const ok = injectViaFileInput(input, file, { DataTransferCtor: FakeDataTransfer });
    expect(ok).toBe(true);
    expect(input.files.length).toBe(1);
    expect(input.files[0]).toBe(file);
    const types = input.dispatchEvent.mock.calls.map((c) => c[0].type);
    expect(types).toEqual(['input', 'change']);
  });

  it('injectViaFileInput：files 赋值被拒（readonly）→ 返回 false（交给 paste 兜底）', () => {
    const file = new File(['x'], 'a.png', { type: 'image/png' });
    const input = {
      get files() { return null; },
      set files(_) { throw new TypeError('readonly'); },
      dispatchEvent: vi.fn(),
    };
    expect(injectViaFileInput(input, file, { DataTransferCtor: FakeDataTransfer })).toBe(false);
    expect(input.dispatchEvent).not.toHaveBeenCalled();
  });

  it('injectViaPaste：paste 事件携带 clipboardData.files（监听器可读）', () => {
    const file = new File(['x'], 'a.png', { type: 'image/png' });
    const editor = document.createElement('div');
    document.body.appendChild(editor);
    const seen = [];
    editor.addEventListener('paste', (e) => {
      seen.push(e.clipboardData.files[0]);
      e.preventDefault();
    });
    const ok = injectViaPaste(editor, file, { DataTransferCtor: FakeDataTransfer });
    expect(ok).toBe(true);
    expect(seen.length).toBe(1);
    expect(seen[0]).toBe(file);
  });

  it('countMediaNodes / waitForMediaInEditor：媒体落地（基线差分）与超时判负', async () => {
    const editor = document.createElement('div');
    document.body.appendChild(editor);
    editor.innerHTML = '<div class="preview">existing</div>'; // 平台自带预览 → 计入基线
    const baseline = countMediaNodes(editor);
    expect(baseline).toBe(1);
    // 不落地：超时判负（轮询真实短时器）
    const miss = await waitForMediaInEditor(editor, { baselineCount: baseline, timeoutMs: 120, pollMs: 20 });
    expect(miss).toBe(false);
    // 落地：媒体节点数增长
    setTimeout(() => { editor.appendChild(Object.assign(document.createElement('img'), { src: 'blob:x' })); }, 30);
    const hit = await waitForMediaInEditor(editor, { baselineCount: baseline, timeoutMs: 800, pollMs: 25 });
    expect(hit).toBe(true);
  });

  it('injectMediaToEditor：paste 注入 + 平台响应（追加 img）→ ok method=paste', async () => {
    const editor = document.createElement('div');
    document.body.appendChild(editor);
    editor.addEventListener('paste', () => {
      const img = document.createElement('img');
      img.src = 'blob:platform';
      editor.appendChild(img);
    });
    const file = new File(['x'], 'a.png', { type: 'image/png' });
    const res = await injectMediaToEditor(editor, file, {
      doc: document, verifyTimeoutMs: 800, pollMs: 25, DataTransferCtor: FakeDataTransfer,
    });
    expect(res).toEqual({ ok: true, method: 'paste' });
  });

  it('injectMediaToEditor：file-input 路径优先（change 后平台渲染 img）→ method=file-input', async () => {
    const panel = document.createElement('div');
    const editor = document.createElement('div');
    editor.setAttribute('contenteditable', 'true');
    const input = document.createElement('input');
    input.type = 'file';
    input.setAttribute('accept', 'image/*');
    // jsdom 的 input.files 是只读同源对象；Chrome 允许 DataTransfer.files 赋值。
    // 用自有可写属性模拟 Chrome 行为（原型 getter 被遮蔽）。
    Object.defineProperty(input, 'files', { value: null, writable: true, configurable: true });
    panel.appendChild(editor);
    panel.appendChild(input);
    document.body.appendChild(panel);
    input.addEventListener('change', () => {
      const img = document.createElement('img');
      img.src = 'blob:platform';
      editor.appendChild(img);
    });
    const file = new File(['x'], 'a.png', { type: 'image/png' });
    const res = await injectMediaToEditor(editor, file, {
      doc: document, verifyTimeoutMs: 800, pollMs: 25, DataTransferCtor: FakeDataTransfer,
    });
    expect(res).toEqual({ ok: true, method: 'file-input' });
  });

  it('injectMediaToEditor：平台不响应（合成事件被忽略）→ ok:false（宁可降级不盲发）', async () => {
    const editor = document.createElement('div');
    document.body.appendChild(editor);
    const file = new File(['x'], 'a.png', { type: 'image/png' });
    const res = await injectMediaToEditor(editor, file, {
      doc: document, verifyTimeoutMs: 150, pollMs: 25, DataTransferCtor: FakeDataTransfer,
    });
    expect(res).toEqual({ ok: false, method: 'paste' });
  });

  it('injectMediaToEditor：file-input 已触发但未落地 → 不再试 paste（防双注入），判失败', async () => {
    const panel = document.createElement('div');
    const editor = document.createElement('div');
    const input = document.createElement('input');
    input.type = 'file';
    input.setAttribute('accept', 'image/*');
    Object.defineProperty(input, 'files', { value: null, writable: true, configurable: true });
    panel.appendChild(editor);
    panel.appendChild(input);
    document.body.appendChild(panel);
    const file = new File(['x'], 'a.png', { type: 'image/png' });
    const res = await injectMediaToEditor(editor, file, {
      doc: document, verifyTimeoutMs: 150, pollMs: 25, DataTransferCtor: FakeDataTransfer,
    });
    expect(res).toEqual({ ok: false, method: 'file-input' });
    // 已触发平台上传但 DOM 未落地 → 不再试 paste 防双注入，method 保留 file-input 为证
  });
});

// ── D. makeSendMediaHook：渠道 hook 工厂行为 ──────────────────────────────
//（真实时器：humanDelay('click') 上限 360ms，可接受；不与 fake timers 互相污染）

function makeHookStubs(over = {}) {
  const editor = { dispatchEvent: vi.fn() };
  const stubs = {
    editor,
    clearEditor: vi.fn(),
    fetchFile: vi.fn(async () => ({ file: { type: 'image/png', name: 'a.png' }, source: 'direct' })),
    injectToEditor: vi.fn(async () => ({ ok: true, method: 'paste' })),
    typeCaption: vi.fn(async () => {}),
    findInput: vi.fn(() => editor),
    findSend: vi.fn(() => ({ id: 'btn' })),
    clickFn: vi.fn(),
    log: { warn: vi.fn(), error: vi.fn(), info: vi.fn() },
  };
  const hook = makeSendMediaHook({
    channelKey: 'douyin_web',
    channelLabel: '抖音',
    findInput: stubs.findInput,
    findSend: stubs.findSend,
    getAccount: () => 'acc1',
    log: stubs.log,
    clickFn: stubs.clickFn,
    fetchFile: stubs.fetchFile,
    injectToEditor: stubs.injectToEditor,
    typeCaption: stubs.typeCaption,
    clearEditor: stubs.clearEditor,
    fetchTimeoutMs: 50,
    verifyTimeoutMs: 50,
    ...over,
  });
  return { stubs, hook };
}

describe('M-3 makeSendMediaHook：发送编排 / 清场 / 弃用语义', () => {
  it('happy path：清空 → 下载 → 注入 → 文案 → 点击发送，返回 {ok, method}', async () => {
    const { stubs, hook } = makeHookStubs();
    const res = await hook('image', 'https://f/i.png', 'conv1', { text: '这是图片', assertConv: () => true });
    expect(res).toEqual({ ok: true, method: 'paste' });
    expect(stubs.clearEditor).toHaveBeenCalledTimes(1);
    expect(stubs.fetchFile).toHaveBeenCalledWith('https://f/i.png', expect.anything());
    expect(stubs.injectToEditor).toHaveBeenCalledTimes(1);
    expect(stubs.typeCaption).toHaveBeenCalledWith(expect.anything(), '这是图片');
    expect(stubs.clickFn).toHaveBeenCalledWith({ id: 'btn' });
  });

  it('无文案不键入（typeCaption 不调用）', async () => {
    const { stubs, hook } = makeHookStubs();
    const res = await hook('image', 'https://f/i.png', 'conv1', {});
    expect(res.ok).toBe(true);
    expect(stubs.typeCaption).not.toHaveBeenCalled();
  });

  it('找不到输入框：抛错（adapter 降级文本）', async () => {
    const { stubs, hook } = makeHookStubs({ findInput: vi.fn(() => null) });
    await expect(hook('image', 'https://f/i.png', 'conv1', {})).rejects.toThrow(/input not found \(sendMedia\)/);
    expect(stubs.clearEditor).not.toHaveBeenCalled();
  });

  it('注入未落地：抛错 + 失败清场（clearEditor 共 2 次：注入前 + 清理），不点发送', async () => {
    const { stubs, hook } = makeHookStubs({
      injectToEditor: vi.fn(async () => ({ ok: false, method: 'paste' })),
    });
    await expect(hook('image', 'https://f/i.png', 'conv1', {})).rejects.toThrow(/媒体注入未落地/);
    expect(stubs.clearEditor).toHaveBeenCalledTimes(2);
    expect(stubs.clickFn).not.toHaveBeenCalled();
  });

  it('下载失败：抛错 + 清场（clearEditor 共 2 次：注入前清空 + 失败清理）', async () => {
    const { stubs, hook } = makeHookStubs({
      fetchFile: vi.fn(async () => { throw new Error('媒体下载失败'); }),
    });
    await expect(hook('image', 'https://f/i.png', 'conv1', {})).rejects.toThrow(/媒体下载失败/);
    expect(stubs.clearEditor).toHaveBeenCalledTimes(2);
    expect(stubs.injectToEditor).not.toHaveBeenCalled();
  });

  it('会话漂移：assertConv=false → conv-drift（清输入框、不发送）', async () => {
    const { stubs, hook } = makeHookStubs();
    await expect(hook('image', 'https://f/i.png', 'conv1', { assertConv: () => false })).rejects.toMatchObject({ code: 'conv-drift' });
    expect(stubs.clickFn).not.toHaveBeenCalled();
    expect(stubs.clearEditor).toHaveBeenCalledTimes(2); // 注入前 + conv-drift 清场
  });

  it('无发送按钮：回车兜底（keydown/keyup Enter）', async () => {
    const { stubs, hook } = makeHookStubs({ findSend: vi.fn(() => null) });
    const res = await hook('image', 'https://f/i.png', 'conv1', { assertConv: () => true });
    expect(res.ok).toBe(true);
    expect(stubs.clickFn).not.toHaveBeenCalled();
    const keys = stubs.editor.dispatchEvent.mock.calls.map((c) => c[0].key);
    expect(keys).toEqual(['Enter', 'Enter']);
  });

  it('abortToken 预先弃用：入口即退出，不下载不注入不清场', async () => {
    const { stubs, hook } = makeHookStubs();
    await expect(hook('image', 'https://f/i.png', 'conv1', { abortToken: { aborted: true, reason: 'x' } }))
      .rejects.toMatchObject({ code: 'send-media-aborted' });
    expect(stubs.fetchFile).not.toHaveBeenCalled();
    expect(stubs.injectToEditor).not.toHaveBeenCalled();
    expect(stubs.clearEditor).not.toHaveBeenCalled();
    expect(stubs.clickFn).not.toHaveBeenCalled();
  });

  it('abortToken 中途弃用（下载期间被降级接管）：注入不执行、失败不清场（不与降级键入抢输入框）', async () => {
    const abortToken = { aborted: false, reason: '' };
    const { stubs, hook } = makeHookStubs({
      fetchFile: vi.fn(async () => {
        abortToken.aborted = true; // 下载期间 adapter 已超时降级
        return { file: { type: 'image/png', name: 'a.png' }, source: 'direct' };
      }),
    });
    await expect(hook('image', 'https://f/i.png', 'conv1', { abortToken }))
      .rejects.toMatchObject({ code: 'send-media-aborted' });
    expect(stubs.injectToEditor).not.toHaveBeenCalled();
    expect(stubs.clearEditor).toHaveBeenCalledTimes(1); // 仅注入前那次；catch 不清场
    expect(stubs.clickFn).not.toHaveBeenCalled();
  });
});

// ── E. background bridge:fetchMedia 代理 ──────────────────────────────────

let bgListeners = [];
function makeChrome() {
  bgListeners = [];
  const chrome = {
    runtime: {
      onMessage: { addListener: (fn) => bgListeners.push(fn) },
    },
    storage: {
      local: {
        get: (_key, cb) => { cb({}); },
        set: (_obj, cb) => { cb && cb(); },
      },
    },
    webNavigation: { onCommitted: { addListener: () => {} } },
    scripting: {},
    tabs: { sendMessage: () => {}, query: (_o, cb) => { cb([]); } },
    alarms: { create: () => {}, onAlarm: { addListener: () => {} } },
  };
  return chrome;
}

// background/index.js 与 sse-client.js 各注册一个 onMessage listener：
// 逐个尝试，返回值 !== false 或已同步 sendResponse 即视为命中（与 background.test.js 同口径）。
function callListener(msg, sendResponse) {
  let called = false;
  const wrapper = (resp) => { called = true; sendResponse(resp); };
  for (const fn of bgListeners) {
    const ret = fn(msg, {}, wrapper);
    if (ret !== false || called) return called;
  }
  return called;
}

describe('M-3 background bridge:fetchMedia 代理', () => {
  beforeEach(() => {
    vi.resetModules();
    globalThis.chrome = makeChrome();
    globalThis.btoa = globalThis.btoa || ((s) => Buffer.from(s, 'binary').toString('base64'));
  });
  afterEach(() => {
    delete globalThis.chrome;
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('非法 URL（非 http/https）→ {ok:false,error:"invalid_url"}，不发 fetch', async () => {
    const fetchSpy = vi.fn();
    vi.stubGlobal('fetch', fetchSpy);
    await import('../src/background/index.js');
    const resp = await new Promise((resolve) => {
      callListener({ type: 'bridge:fetchMedia', url: 'ftp://x/y.png' }, resolve);
    });
    expect(resp).toEqual({ ok: false, error: 'invalid_url' });
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it('合法 URL：代取字节 → base64/mime/bytes 回传', async () => {
    const payload = new TextEncoder().encode('media-bytes');
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: { get: (k) => (k === 'content-type' ? 'image/png' : k === 'content-length' ? String(payload.byteLength) : null) },
      arrayBuffer: async () => payload.buffer.slice(0),
    })));
    await import('../src/background/index.js');
    const resp = await new Promise((resolve) => {
      callListener({ type: 'bridge:fetchMedia', url: 'https://cdn.example.com/a.png' }, resolve);
    });
    expect(resp.ok).toBe(true);
    expect(resp.mime).toBe('image/png');
    expect(resp.bytes).toBe(payload.byteLength);
    expect(Buffer.from(resp.base64, 'base64').toString()).toBe('media-bytes');
  });

  it('超上限（maxBytes 截到上限内仍超）→ {ok:false,error:"too_large"}', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      status: 200,
      headers: { get: (k) => (k === 'content-length' ? String(30 * 1024 * 1024) : null) },
      arrayBuffer: async () => new ArrayBuffer(30 * 1024 * 1024),
    })));
    await import('../src/background/index.js');
    const resp = await new Promise((resolve) => {
      callListener({ type: 'bridge:fetchMedia', url: 'https://cdn.example.com/big.mp4' }, resolve);
    });
    expect(resp.ok).toBe(false);
    expect(resp.error).toMatch(/too_large/);
  });

  it('上游 HTTP 404：如实回 {ok:false,error:"HTTP 404"}（content 端据此降级文本）', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 404, headers: { get: () => null } })));
    await import('../src/background/index.js');
    const resp = await new Promise((resolve) => {
      callListener({ type: 'bridge:fetchMedia', url: 'https://cdn.example.com/missing.png' }, resolve);
    });
    expect(resp).toEqual({ ok: false, error: 'HTTP 404' });
  });
});
