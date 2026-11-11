import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../src/core/humanize.js', () => ({
  humanDelay: async () => {},
}));

import { buildBilibiliAdapter, getAccountId } from '../src/channels/bilibili.js';
import { SENDER } from '../src/core/types.js';

function stubLocation(url) {
  const u = new URL(url);
  vi.stubGlobal('location', {
    href: u.href,
    pathname: u.pathname,
    search: u.search,
    hash: u.hash,
    hostname: u.hostname,
    host: u.host,
  });
}

// 2026-11-06 fixture 对齐 ChatGPT-On-CS v1.4.5 实证 DOM：
// .list-container > .list-item（.notify.notify-number 未读）、
// .dialog（.title > span 用户名）、.message-list > .msg-item
// （class not-me / .message-content class is-me）、#editor + btn-box send-btn。
function fixture() {
  document.body.innerHTML = `
    <div class="list-container">
      <div class="list-item"><span class="name">未关注人</span></div>
      <div class="list-item"><span class="name">测试用户</span><div class="notify notify-number">2</div></div>
    </div>
    <div class="dialog">
      <div class="title"><span>测试用户</span></div>
      <div class="message-list">
        <div class="msg-item not-me"><div class="message-content">你好，在吗</div></div>
        <div class="msg-item"><div class="message-content is-me">在的</div></div>
        <div class="msg-item not-me"><div class="message-content"><img src="https://i0.hdslb.com/bfs/x.jpg"></div></div>
      </div>
      <div class="editor-box">
        <div id="editor" contenteditable="true"></div>
        <button class="btn-box send-btn">发送</button>
      </div>
    </div>
  `;
}

describe('bilibili adapter（实验渠道，选择器对齐 ChatGPT-On-CS 实证）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://message.bilibili.com/#/whisper');
    fixture();
  });

  it('match 命中私信页；会话列表取 .list-item', () => {
    const a = buildBilibiliAdapter();
    expect(a.match()).toBe(true);
    const list = a.getConversationList();
    expect(list.length).toBe(2);
    expect(list[0].name).toBe('未关注人');
    expect(list[1].name).toBe('测试用户');
  });

  it('getConversationId 兜底用 .dialog 面板昵称派生', () => {
    const a = buildBilibiliAdapter();
    expect(a.getConversationId()).toBe('conv:测试用户');
  });

  it('parseMessageItem 按 not-me/is-me 区分方向并识别图片', () => {
    const a = buildBilibiliAdapter();
    const items = a.getMessageItems();
    expect(items.length).toBe(3);
    const other = a.parseMessageItem(items[0]);
    expect(other.sender_type).toBe(SENDER.CUSTOMER);
    expect(other.text).toBe('你好，在吗');
    expect(a.parseMessageItem(items[1]).sender_type).toBe(SENDER.SELF);
    const imgMsg = a.parseMessageItem(items[2]);
    expect(imgMsg.msg_type).toBe('image');
    expect(imgMsg.media_url).toContain('hdslb.com');
  });

  it('sendText 写入 #editor 并点击 btn-box send-btn', async () => {
    const a = buildBilibiliAdapter();
    const input = document.querySelector('#editor');
    const btn = document.querySelector('button.btn-box.send-btn');
    let seen = '';
    btn.addEventListener('click', () => { seen = input.innerText || input.textContent || ''; });
    await a.hooks.sendText('测试回复', {});
    expect(seen).toBe('测试回复');
    expect(input.innerText || input.textContent || '').toBe('');
  });

  it('getAccountId 无登录态时返回稳定占位而非空串', () => {
    expect(getAccountId()).toBe('bilibili-unknown');
  });
});

// M-4（2026-11-11）：bilibili 媒体分支补齐（审计登记「仅 image」→ 补 video/voice/emoji/link/card）
// + IMAGE_UPLOAD 死选择器删除决策。fixture 口径（模板同 douyin/xhs 主力渠道），
// Experimental 渠道真机未校准（BR-1 同源缺口）。
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

describe('M-4 bilibili 媒体分支补齐', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://message.bilibili.com/#/whisper');
    document.body.innerHTML = '';
  });

  const biItem = (html) => {
    document.body.innerHTML = `<div class="message-list">${html}</div>`;
    return document.querySelector('.message-list > div');
  };

  it('视频 video[src] → msg_type=video + media_url', () => {
    const a = buildBilibiliAdapter();
    const p = a.parseMessageItem(biItem('<div class="msg-item not-me"><div class="message-content"><video src="https://i.hdslb.com/v.mp4"></video></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('video');
    expect(p.media_url).toBe('https://i.hdslb.com/v.mp4');
  });

  it('语音 audio[src] → msg_type=voice + media_url；提取不到保持类型标注不丢弃', () => {
    const a = buildBilibiliAdapter();
    const withUrl = a.parseMessageItem(biItem('<div class="msg-item not-me"><div class="message-content"><div class="voice"><audio src="https://i.hdslb.com/v.m4a"></audio></div></div></div>'));
    expect(withUrl).not.toBeNull();
    expect(withUrl.msg_type).toBe('voice');
    expect(withUrl.media_url).toBe('https://i.hdslb.com/v.m4a');
    // 纯语音无 URL：旧门禁「!text && !mediaUrl → null」整条丢弃；M-4 放宽后保留类型标注
    const noUrl = a.parseMessageItem(biItem('<div class="msg-item not-me"><div class="message-content"><div class="voice"></div></div></div>'));
    expect(noUrl).not.toBeNull();
    expect(noUrl.msg_type).toBe('voice');
    expect(noUrl.media_url).toBe('');
    expect(noUrl.text).toBe('[语音]');
  });

  it('表情容器 data-src → emoji + media_url；font-icon 无 src → 仅类型标注', () => {
    const a = buildBilibiliAdapter();
    const withUrl = a.parseMessageItem(biItem('<div class="msg-item not-me"><div class="message-content"><span class="emoji" data-src="//i.hdslb.com/e.png"></span></div></div>'));
    expect(withUrl.msg_type).toBe('emoji');
    expect(withUrl.media_url).toBe('https://i.hdslb.com/e.png');
    const fontIcon = a.parseMessageItem(biItem('<div class="msg-item not-me"><div class="message-content"><span class="emoji emoji-smile"></span></div></div>'));
    expect(fontIcon).not.toBeNull();
    expect(fontIcon.msg_type).toBe('emoji');
    expect(fontIcon.media_url).toBe('');
  });

  it('纯链接（空锚点）→ msg_type=link + href 绝对化', () => {
    const a = buildBilibiliAdapter();
    const p = a.parseMessageItem(biItem('<div class="msg-item not-me"><div class="message-content"><a href="https://b23.tv/x"></a></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('link');
    expect(p.media_url).toBe('https://b23.tv/x');
  });

  it('卡片（标题/缩略图）→ msg_type=card，标题进 content', () => {
    const a = buildBilibiliAdapter();
    const p = a.parseMessageItem(biItem('<div class="msg-item not-me"><div class="message-content"><div class="card"><div class="title">链接标题</div><img src="https://i.hdslb.com/t.jpg"></div></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('card');
    expect(p.text).toBe('链接标题');
    expect(p.media_url).toBe('https://i.hdslb.com/t.jpg');
  });

  it('回归：头像图不误判为消息图（文本+头像 → text + media_url 空）', () => {
    const a = buildBilibiliAdapter();
    const p = a.parseMessageItem(biItem('<div class="msg-item not-me"><div class="message-content"><img class="avatar" src="https://i.hdslb.com/av.jpg"><span>你好</span></div></div>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('text');
    expect(p.media_url).toBe('');
    expect(p.text).toBe('你好');
  });

  it('IMAGE_UPLOAD 死选择器已删除（源码契约，M-1 死代码清除同论证模式）', () => {
    const src = readFileSync(join(process.cwd(), 'src', 'channels', 'bilibili.js'), 'utf8');
    expect(src).not.toMatch(/IMAGE_UPLOAD\s*:/);
  });
});
