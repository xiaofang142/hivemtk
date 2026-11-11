import { describe, it, expect, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { buildKuaishouAdapter, getAccountId } from '../src/channels/kuaishou.js';
import { isExperimentalChannel, EXPERIMENTAL_CHANNELS } from '../src/core/constants.js';

// B8（批3）：kuaishou 能力如实降级 + getAccountId 路径末段误判修复。

describe('B8 getAccountId 只信 /profile/<id> 形态', () => {
  beforeEach(() => {
    localStorage.clear();
    history.replaceState(null, '', '/new-reco');
  });

  it('私信默认路径 /new-reco 不再被当成账号 id（旧正则 ≥6 字符必误判）', () => {
    expect(getAccountId()).toBe('');
    expect(localStorage.getItem('hivebridge:account:kuaishou')).toBe(null);
  });

  it('/profile/<userId> 提取并缓存', () => {
    history.replaceState(null, '', '/profile/3xyABC123456');
    expect(getAccountId()).toBe('3xyABC123456');
    expect(localStorage.getItem('hivebridge:account:kuaishou')).toBe('3xyABC123456');
  });

  it('非 profile 路径回落缓存值（跨页保持身份）', () => {
    localStorage.setItem('hivebridge:account:kuaishou', 'cached-id-1');
    history.replaceState(null, '', '/message/list');
    expect(getAccountId()).toBe('cached-id-1');
  });

  it('/profile/ 带 query 也能取到纯 id', () => {
    history.replaceState(null, '', '/profile/3zzTopLevel?k=1');
    expect(getAccountId()).toBe('3zzTopLevel');
  });
});

describe('B8 实验渠道声明', () => {
  it('kuaishou 与 2026-11 新增渠道是实验渠道；其余不拦截', () => {
    expect(isExperimentalChannel('kuaishou')).toBe(true);
    expect(isExperimentalChannel('jd')).toBe(true);
    expect(isExperimentalChannel('bilibili')).toBe(true);
    expect(isExperimentalChannel('weibo')).toBe(true);
    // CH-16 首切片（2026-11-09）：pdd 选择器为 X 级历史源码实证未真机校准，同标实验。
    expect(isExperimentalChannel('pdd')).toBe(true);
    expect(isExperimentalChannel('douyin')).toBe(false);
    expect(isExperimentalChannel(undefined)).toBe(false);
    expect(Object.keys(EXPERIMENTAL_CHANNELS)).toEqual(['kuaishou', 'jd', 'bilibili', 'weibo', 'pdd']);
  });

  it('content 启动路径引用实验告警（源码契约）', () => {
    const src = readFileSync(join(process.cwd(), 'src', 'content', 'common.js'), 'utf8');
    expect(src).toContain('isExperimentalChannel');
    expect(src).toContain('实验渠道');
  });
});

// M-4（2026-11-11）：kuaishou 媒体分支补齐（审计缺口 voice/emoji/link/card）。
// fixture 口径（模板同 douyin/xhs 主力渠道），真机未校准（BR-1 同源缺口）。
describe('M-4 kuaishou 媒体分支补齐', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  const ksItem = (html) => {
    document.body.innerHTML = `<div class="chat-content">${html}</div>`;
    return document.querySelector('.chat-content > div');
  };

  it('语音 audio[src] → msg_type=voice + media_url（此前整条仅类型标注）', () => {
    const a = buildKuaishouAdapter();
    const p = a.parseMessageItem(ksItem('<div class="message-item"><div class="voice-wrap"><audio src="https://ks.im/v.m4a"></audio></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('https://ks.im/v.m4a');
  });

  it('语音容器 data-src 兜底提取（无 audio 元素形态）', () => {
    const a = buildKuaishouAdapter();
    const p = a.parseMessageItem(ksItem('<div class="message-item"><div class="voice-wrap" data-src="//ks.im/x.m4a"></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('http://ks.im/x.m4a');
  });

  it('语音提取不到 URL → 保持 voice 类型标注（media_url 空，消息不丢）', () => {
    const a = buildKuaishouAdapter();
    const p = a.parseMessageItem(ksItem('<div class="message-item"><div class="voice-wrap"></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('voice');
    expect(p.media_url).toBe('');
  });

  it('表情容器 data-src → emoji + media_url；font-icon 无 src → 仅类型标注', () => {
    const a = buildKuaishouAdapter();
    const withUrl = a.parseMessageItem(ksItem('<div class="message-item"><span class="emoji" data-src="//ks.cdn/e.png"></span></div>'));
    expect(withUrl.msg_type).toBe('emoji');
    expect(withUrl.media_url).toBe('http://ks.cdn/e.png');
    const fontIcon = a.parseMessageItem(ksItem('<div class="message-item"><span class="emoji emoji-smile"></span></div>'));
    expect(fontIcon).not.toBeNull();
    expect(fontIcon.msg_type).toBe('emoji');
    expect(fontIcon.media_url).toBe('');
  });

  it('纯链接（空锚点）→ msg_type=link + href 绝对化', () => {
    const a = buildKuaishouAdapter();
    const p = a.parseMessageItem(ksItem('<div class="message-item"><a href="https://m.kuaishou.com/x"></a></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('link');
    expect(p.media_url).toBe('https://m.kuaishou.com/x');
  });

  it('卡片（标题/摘要/缩略图）→ msg_type=card，摘要进 content', () => {
    const a = buildKuaishouAdapter();
    const p = a.parseMessageItem(ksItem('<div class="message-item"><div class="card"><div class="title">商品A</div><div class="info">￥9.9</div><img src="https://ks.cdn/t.jpg"></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('card');
    expect(p.text).toBe('￥9.9');
    expect(p.media_url).toBe('https://ks.cdn/t.jpg');
  });

  it('回归：头像图不误判为消息图（文本+头像 → text + media_url 空）', () => {
    const a = buildKuaishouAdapter();
    const p = a.parseMessageItem(ksItem('<div class="message-item"><img class="avatar" src="https://ks.cdn/av.jpg"><span class="text-content">你好</span></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('text');
    expect(p.media_url).toBe('');
  });

  it('回归：卡内头像图不误判为卡片缩略图（card + media_url 空）', () => {
    const a = buildKuaishouAdapter();
    const p = a.parseMessageItem(ksItem('<div class="message-item"><div class="card"><img class="avatar" src="https://ks.cdn/av.jpg"><div class="title">商品B</div></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('card');
    expect(p.media_url).toBe('');
    expect(p.text).toBe('商品B');
  });

  it('回归：撤回文案仍走 system（M-4 分支顺序不破坏既有口径）', () => {
    // 既有口径：文本型撤回先被 isSystemMessage（SYSTEM_TEXT_PATTERNS 含「撤回了?一条消息」）
    // 拦截归 system；extractMessageContent 的 recall 分支仅服务 DOM 结构型撤回。
    // M-4 未触碰该顺序，此处钉死防回归。
    const a = buildKuaishouAdapter();
    const p = a.parseMessageItem(ksItem('<div class="message-item"><span class="text-content">对方撤回了一条消息</span></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('system');
  });
});
