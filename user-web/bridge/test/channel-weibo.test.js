import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../src/core/humanize.js', () => ({
  humanDelay: async () => {},
}));

import { buildWeiboAdapter, getAccountId } from '../src/channels/weibo.js';
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

function fixture() {
  document.body.innerHTML = `
    <div class="chatlist-box">
      <ul class="contactslist">
        <li class="sessionlist active" data-uid="20001"><div class="name">微博好友</div></li>
        <li class="sessionlist" data-uid="20002"><div class="name">另一个好友</div></li>
      </ul>
    </div>
    <div class="chat-header"><div class="friendname one-line hvhd"><div><span>微博好友</span></div></div></div>
    <ul class="chat-content">
      <li class="message-item"><div class="bubble_cont basi left"><p class="puretext">在吗</p></div></li>
      <li class="message-item"><div class="bubble_cont basi right"><p class="puretext">在的</p></div></li>
      <li class="message-item"><div class="bubble_cont basi left"><div class="card"><div><img src="https://wx1.sinaimg.cn/x.jpg"></div></div></div></li>
    </ul>
    <textarea id="webchat-textarea"></textarea>
  `;
}

describe('weibo adapter（实验渠道）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://weibo.com/chat');
    fixture();
  });

  it('match 命中聊天页；会话列表去重', () => {
    const a = buildWeiboAdapter();
    expect(a.match()).toBe(true);
    const list = a.getConversationList();
    expect(list.length).toBe(2);
    expect(list[0].id).toBe('20001');
  });

  it('getConversationId 取活动会话 data-uid', () => {
    const a = buildWeiboAdapter();
    expect(a.getConversationId()).toBe('20001');
  });

  it('parseMessageItem 按气泡 left/right 区分方向（ChatGPT-On-CS 实证）并识别图片', () => {
    const a = buildWeiboAdapter();
    const items = a.getMessageItems();
    expect(items.length).toBe(3);
    expect(a.parseMessageItem(items[0]).sender_type).toBe(SENDER.CUSTOMER);
    expect(a.parseMessageItem(items[1]).sender_type).toBe(SENDER.SELF);
    expect(a.parseMessageItem(items[0]).text).toBe('在吗');
    const imgMsg = a.parseMessageItem(items[2]);
    expect(imgMsg.msg_type).toBe('image');
    expect(imgMsg.media_url).toContain('sinaimg.cn');
    expect(imgMsg.sender_type).toBe(SENDER.CUSTOMER);
  });

  it('sendText 用 Enter 发送并清空输入框', async () => {
    const a = buildWeiboAdapter();
    const input = document.querySelector('#webchat-textarea');
    let seen = '';
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') seen = input.value;
    });
    await a.hooks.sendText('微博回复', {});
    expect(seen).toBe('微博回复');
    expect(input.value).toBe('');
  });

  it('getAccountId 无登录态时返回稳定占位', () => {
    expect(getAccountId()).toBe('weibo-unknown');
  });
});

// M-4（2026-11-11）：weibo 媒体分支补齐（审计缺口 voice/card/link）。
// fixture 口径（模板同 douyin/xhs 主力渠道 + 实证 CARD_IMG/CARD_VIDEO 同源 div.card），
// Experimental 渠道真机未校准（BR-1 同源缺口）。
describe('M-4 weibo 媒体分支补齐', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://weibo.com/chat');
    document.body.innerHTML = '';
  });

  const wbItem = (html) => {
    document.body.innerHTML = `<ul class="chat-content">${html}</ul>`;
    return document.querySelector('.chat-content > li');
  };

  it('卡片（链接卡：标题/缩略图）→ msg_type=card，标题进 content，缩略图进 media_url', () => {
    const a = buildWeiboAdapter();
    const p = a.parseMessageItem(wbItem('<li class="message-item"><div class="bubble_cont basi left"><div class="card"><div><img src="https://wx1.sinaimg.cn/t.jpg"></div><div class="title">链接标题</div></div></div></li>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('card');
    expect(p.text).toBe('链接标题');
    expect(p.media_url).toBe('https://wx1.sinaimg.cn/t.jpg');
    expect(p.sender_type).toBe(SENDER.CUSTOMER);
  });

  it('语音 audio[src] → msg_type=voice + media_url；提取不到保持类型标注不丢弃', () => {
    const a = buildWeiboAdapter();
    const withUrl = a.parseMessageItem(wbItem('<li class="message-item"><div class="bubble_cont basi left"><div class="voice"><audio src="https://w.im/v.m4a"></audio></div></div></li>'));
    expect(withUrl).not.toBeNull();
    expect(withUrl.msg_type).toBe('voice');
    expect(withUrl.media_url).toBe('https://w.im/v.m4a');
    // 纯语音无 URL：旧门禁「!text && !mediaUrl → null」整条丢弃；M-4 放宽后保留类型标注
    const noUrl = a.parseMessageItem(wbItem('<li class="message-item"><div class="bubble_cont basi left"><div class="voice"></div></div></li>'));
    expect(noUrl).not.toBeNull();
    expect(noUrl.msg_type).toBe('voice');
    expect(noUrl.media_url).toBe('');
    expect(noUrl.text).toBe('[语音]');
  });

  it('纯链接（空锚点）→ msg_type=link + href 绝对化', () => {
    const a = buildWeiboAdapter();
    const p = a.parseMessageItem(wbItem('<li class="message-item"><div class="bubble_cont basi left"><a href="https://m.weibo.cn/x"></a></div></li>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('link');
    expect(p.media_url).toBe('https://m.weibo.cn/x');
  });

  it('回归：纯图卡（div.card 无标题摘要）仍归 image（实证口径不被 card 分支抢走）', () => {
    const a = buildWeiboAdapter();
    const p = a.parseMessageItem(wbItem('<li class="message-item"><div class="bubble_cont basi left"><div class="card"><div><img src="https://wx1.sinaimg.cn/x.jpg"></div></div></div></li>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('image');
    expect(p.media_url).toBe('https://wx1.sinaimg.cn/x.jpg');
  });

  it('回归：头像图不误判为消息图（文本+头像 → text + media_url 空）', () => {
    const a = buildWeiboAdapter();
    const p = a.parseMessageItem(wbItem('<li class="message-item"><div class="bubble_cont basi left"><img class="avatar" src="https://wx1.sinaimg.cn/av.jpg"><p class="puretext">你好</p></div></li>'));
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('text');
    expect(p.media_url).toBe('');
    expect(p.text).toBe('你好');
  });
});
