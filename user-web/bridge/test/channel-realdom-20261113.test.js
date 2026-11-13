// 2026-11-13 真机缺口修复回归（ego-browser 复用登录态只读核对取得的现网 DOM 实证）。
// 覆盖四渠道五个断点：
//   B站  P0-1 上行双断点（_Msg_ 哈希类 + getConversationId 全层失效）
//        P1-2 会话列表污染（54 子元素 → 6 根行）
//        P2-8 会话 id 昵称派生 / 我的消息串台
//        鸡生蛋（bot 会话无输入框 → match=false）
//   小红书 P1-3 系统消息漏斗（24小时最多发1条 / 暂不支持该消息类型）
//   微博  P1-5 鸡生蛋（未开会话 match=false）
//        P2-7 weibo-unknown 占位归并失真
//        P2-8 会话 id 昵称派生
//   抖音 P2-6 账号身份错误（会话项 /user/ 链接是对端用户 → 污染缓存）
// fixture 全部按真机实测类名/结构复刻（含 CSS Modules 哈希后缀），不是想象结构。
import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../src/core/humanize.js', () => ({
  humanDelay: async () => {},
}));

import { buildBilibiliAdapter } from '../src/channels/bilibili.js';
import { buildXhsAdapter } from '../src/channels/xhs.js';
import { buildWeiboAdapter, getAccountId as weiboGetAccountId } from '../src/channels/weibo.js';
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

// —— B站现网 fixture（message.bilibili.com/#/whisper 实测类名）——
// 会话根行 class="_SessionItem_t04g4_1"（data-id="contact_<mid>"），子元素
// _SessionItem__Avatar/__Main/__Name/__Info/__Message/__Remove/__RemoveBtn；
// 激活行追加 _SessionItemIsActive_t04g4_12；消息泡外层 _Msg_o7f0t_1，
// 子元素 _Msg__Time/__Wrap/__Main/__Content_o7f0t_12；header 标题为页面级「我的消息」。
function biliLiveFixture({ active = true } = {}) {
  const rows = [
    { mid: '844424930131964', name: '社区中心' },
    { mid: '12076317', name: '哔哩哔哩智能机' },
    { mid: '844424930131965', name: '英雄联盟赛事' },
    { mid: '844424930131966', name: '直播通知' },
    { mid: '844424930131967', name: '云视听小电视-TV' },
    { mid: '844424930131968', name: '会员购' },
  ];
  const list = rows.map((r, i) => `
    <div class="_SessionItem_t04g4_1${active && i === 0 ? ' _SessionItemIsActive_t04g4_12' : ''}" data-id="contact_${r.mid}">
      <div class="_SessionItem__Avatar_t04g4_2"><img src="https://i0.hdslb.com/bfs/face/${i}.jpg"></div>
      <div class="_SessionItem__Main_t04g4_3">
        <div class="_SessionItem__Title_t04g4_9">${r.name}</div>
        <div class="_SessionItem__Name_t04g4_4">${r.name}</div>
        <div class="_SessionItem__Info_t04g4_5">官方账号</div>
        <div class="_SessionItem__Message_t04g4_6">欢迎使用</div>
        <div class="_SessionItem__Remove_t04g4_7"><button class="_SessionItem__RemoveBtn_t04g4_8">删除</button></div>
      </div>
    </div>`).join('');
  document.body.innerHTML = `
    <div class="_SessionList_abc_1">${list}</div>
    <div class="bili-header"><a href="//space.bilibili.com/477764171">个人空间</a></div>
    <div class="_MessageList_1vip7_1 push-setting-on">
      <div class="_Msg_o7f0t_1">
        <div class="_Msg__Time_o7f0t_25">今天 09:13</div>
        <div class="_Msg__Wrap_o7f0t_26"><div class="_Msg__Main_o7f0t_27"><div class="_Msg__Content_o7f0t_12">登录操作通知 账号在新设备或平台登录成功</div></div></div>
      </div>
      <div class="_Msg_o7f0t_1">
        <div class="_Msg__Time_o7f0t_25">今天 09:20</div>
        <div class="_Msg__Wrap_o7f0t_26"><div class="_Msg__Main_o7f0t_27"><div class="_Msg__Content_o7f0t_12">在吗</div></div></div>
      </div>
      <div class="_Msg_o7f0t_1">
        <div class="_Msg__Wrap_o7f0t_26"><div class="_Msg__Main_o7f0t_27"><div class="_Msg__Content_o7f0t_12">有现货吗</div></div></div>
      </div>
    </div>
    <div class="message-header"><div class="title">我的消息</div></div>
  `;
}

describe('B站现网哈希类 DOM（P0-1 上行双断点修复）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://message.bilibili.com/#/whisper/mid844424930131964');
    biliLiveFixture();
  });

  it('getMessageItems 命中 _Msg_ 哈希类并按 outermost 去嵌套（10 节点 → 3 气泡）', () => {
    const a = buildBilibiliAdapter();
    const items = a.getMessageItems();
    // 3 个气泡根（登录操作通知那条被系统漏斗剥掉 → 见下一条）
    expect(items.length).toBe(2);
    for (const it of items) {
      expect(it.className).toContain('_Msg_o7f0t_1');
      expect(it.className).not.toContain('_Msg__Content');
      expect(it.className).not.toContain('_Msg__Time');
    }
  });

  it('官方 bot「登录操作通知」走系统漏斗，不上行触发 AI', () => {
    const a = buildBilibiliAdapter();
    const bubbles = Array.from(document.querySelectorAll('[class*="_Msg_" i]'))
      .filter((el) => !el.parentElement || !el.parentElement.className || !String(el.parentElement.className).includes('_Msg_'));
    const sysBubble = bubbles[0];
    const parsed = a.parseMessageItem(sysBubble);
    expect(parsed).not.toBeNull();
    expect(parsed.msg_type).toBe('system');
    expect(parsed.sender_type).toBe(SENDER.SYSTEM);
    expect(parsed.text).toContain('登录操作通知');
    // 系统消息必须在 getMessageItems 之前就被剥掉（不进上行）
    const items = a.getMessageItems();
    expect(items.some((it) => String(it.className).includes('_Msg_') && it.textContent.includes('登录操作通知'))).toBe(false);
  });

  it('正文文本候选 _Msg__Content 命中（parse 出真实文本而非时间前缀）', () => {
    const a = buildBilibiliAdapter();
    const items = a.getMessageItems();
    expect(items.length).toBe(2);
    const first = a.parseMessageItem(items[0]);
    expect(first.text).toBe('在吗');
    expect(first.msg_type).toBe('text');
  });

  it('getConversationList 只留 6 个根行（54 子元素被 outermost 剥掉）', () => {
    const a = buildBilibiliAdapter();
    const list = a.getConversationList();
    expect(list.length).toBe(6);
    expect(list.map((x) => x.id)).toEqual([
      '844424930131964', '12076317', '844424930131965',
      '844424930131966', '844424930131967', '844424930131968',
    ]);
    expect(list[0].name).toBe('社区中心');
    // 巡检点击目标必须是根行（含删除按钮的子元素曾因无 name 侥幸跳过，仍是错误目标）
    for (const item of list) {
      expect(item.el.className).toContain('_SessionItem_t04g4_1');
      expect(item.el.className).not.toContain('__');
    }
  });

  it('itemId 去 contact_ 前缀归一，与 hash 路由 mid 一致', () => {
    const a = buildBilibiliAdapter();
    const list = a.getConversationList();
    expect(list[0].id).toBe('844424930131964');
    expect(a.getConversationId()).toBe('844424930131964');
  });

  it('active 判定命中 _SessionItemIsActive_t04g4_12（classList.contains 精确匹配的替代）', () => {
    const a = buildBilibiliAdapter();
    expect(a.getConversationId()).toBe('844424930131964');
  });

  it('无 active 类时 hash 路由层兜底（#/whisper/mid844424930131964）', () => {
    biliLiveFixture({ active: false });
    const a = buildBilibiliAdapter();
    expect(a.getConversationId()).toBe('844424930131964');
  });

  it('getPeerName 排除页面级「我的消息」，不产生常数 conv:我的消息 串台', () => {
    const a = buildBilibiliAdapter();
    expect(a.hooks.getPeerName()).toBe('');
    biliLiveFixture({ active: false });
    const b = buildBilibiliAdapter();
    // 全层失效时不返回 'conv:我的消息'
    expect(b.getConversationId()).toBe('844424930131964');
  });

  it('getPeerAvatar 取激活会话行 _SessionItem__Avatar 内 img', () => {
    const a = buildBilibiliAdapter();
    expect(a.hooks.getPeerAvatar()).toBe('https://i0.hdslb.com/bfs/face/0.jpg');
  });

  it('bot-only 账号（无输入框）仅列表+消息列表即 match=true（鸡生蛋修复）', () => {
    const a = buildBilibiliAdapter();
    expect(document.querySelector('textarea, [contenteditable="true"]')).toBeNull();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('strict');
  });

  it('getAccountId 从 space.bilibili.com 链接取 mid（现网可用，无需改）', () => {
    const a = buildBilibiliAdapter();
    expect(a.getAccountId()).toBe('477764171');
  });
});

// —— 小红书系统消息漏斗（P1-3）——
describe('小红书系统消息漏斗（P1-3 修复）', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  const xhsItem = (html) => {
    const el = document.createElement('div');
    el.className = 'chat-item';
    el.innerHTML = html;
    return el;
  };

  it('「24小时内最多只能发1条文字消息」风控提示 → system（不再触发 AI）', () => {
    const a = buildXhsAdapter();
    const parsed = a.parseMessageItem(xhsItem(
      '<div class="xhs-im-bubble__text">对方关注或回复你之前，24小时内最多只能发1条文字消息</div>'
    ));
    expect(parsed).not.toBeNull();
    expect(parsed.msg_type).toBe('system');
    expect(parsed.sender_type).toBe(SENDER.SYSTEM);
    expect(parsed.text).toContain('24小时内最多只能发');
  });

  it('「暂不支持该消息类型，请到手机端查看」兼容卡片 → system（卡片分支之前判定）', () => {
    const a = buildXhsAdapter();
    const parsed = a.parseMessageItem(xhsItem(
      '<div class="card_container"><div class="card_bottom_title">暂不支持该消息类型，请到手机端查看</div></div>'
    ));
    expect(parsed).not.toBeNull();
    expect(parsed.msg_type).toBe('system');
    expect(parsed.sender_type).toBe(SENDER.SYSTEM);
  });

  it('正常客户消息不受影响（漏斗不收窄）', () => {
    const a = buildXhsAdapter();
    const parsed = a.parseMessageItem(xhsItem(
      '<div class="left"><div class="xhs-im-bubble__text">这个还有货吗</div></div>'
    ));
    expect(parsed).not.toBeNull();
    expect(parsed.msg_type).toBe('text');
    expect(parsed.sender_type).toBe('customer');
    expect(parsed.text).toBe('这个还有货吗');
  });
});

// —— 微博（P1-5 / P2-7 / P2-8）——
describe('微博现网 DOM（api.weibo.com/chat）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://api.weibo.com/chat#/chat');
  });

  // 真机实证：自我盒 .user 唯一（.user .hidden=7911766916）；
  // 会话项 <li class="sessionlist relative flex align-items_c"> 无 data-uid/active 类，
  // .name 盒内嵌 <div class="hidden">2671109275</div>（display:none）即对方 uid。
  function weiboLiveFixture() {
    document.body.innerHTML = `
      <div class="user flex justify-c_space-b align-items_c">
        <span class="name one-line hvhd">用户7911766916</span>
        <div class="hidden">7911766916</div>
      </div>
      <div class="chatlist-box">
        <ul class="contactslist">
          <li class="sessionlist relative flex align-items_c">
            <div class="name">新手指南<div class="hidden">2671109275</div></div>
          </li>
          <li class="sessionlist relative flex align-items_c">
            <div class="name">活动通知<div class="hidden">4771252927990411</div></div>
          </li>
        </ul>
      </div>
      <div class="chat-header"><div class="friendname one-line hvhd"><div><span>新手指南</span></div></div></div>
    `;
  }

  it('未开会话（无输入框）chat URL + 会话列表即 match=true（鸡生蛋修复）', () => {
    weiboLiveFixture();
    const a = buildWeiboAdapter();
    expect(document.querySelector('#webchat-textarea')).toBeNull();
    expect(a.match()).toBe(true);
    expect(a.matchMode()).toBe('strict');
  });

  it('getAccountId 从 .user 盒隐藏 uid 取账号（不再 weibo-unknown 归并失真）', () => {
    weiboLiveFixture();
    expect(weiboGetAccountId()).toBe('7911766916');
    expect(localStorage.getItem('hivebridge:account:weibo')).toBe('7911766916');
  });

  it('getAccountId 昵称兜底「用户7911766916」→ uid', () => {
    document.body.innerHTML = `
      <div class="user"><span class="name">用户7911766916</span></div>`;
    expect(weiboGetAccountId()).toBe('7911766916');
  });

  it('会话 id 取 .name 盒隐藏 uid（不再昵称派生 conv:）', () => {
    weiboLiveFixture();
    const a = buildWeiboAdapter();
    const list = a.getConversationList();
    expect(list.length).toBe(2);
    expect(list[0].id).toBe('2671109275');
    expect(list[1].id).toBe('4771252927990411');
  });

  it('itemName 剥离尾部隐藏 uid 串（textContent 环境）', () => {
    weiboLiveFixture();
    const a = buildWeiboAdapter();
    const list = a.getConversationList();
    expect(list[0].name).toBe('新手指南');
    expect(list[1].name).toBe('活动通知');
  });

  it('getConversationId：header 昵称反查列表项 uid（无 /u/ 链接、无 active 类）', () => {
    weiboLiveFixture();
    const a = buildWeiboAdapter();
    expect(a.getConversationId()).toBe('2671109275');
  });

  it('反查不到时才回落 conv:<昵称>（不丢会话）', () => {
    // 列表项不带 .name（不抢 PEER_NAME 文档序优先权）且昵称与 header 不一致
    document.body.innerHTML = `
      <div class="chatlist-box"><ul class="contactslist">
        <li class="sessionlist" data-uid="2671109275"><div class="nick">列表昵称</div></li>
      </ul></div>
      <div class="chat-header"><div class="friendname one-line hvhd"><div><span>陌生昵称</span></div></div></div>`;
    const a = buildWeiboAdapter();
    expect(a.getConversationId()).toBe('conv:陌生昵称');
  });
});

// —— 抖音账号身份（P2-6 升级为账号身份错误）——
describe('douyin getAccountId SSR 优先级（P2-6 修复）', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    try { localStorage.clear(); } catch (e) {  }
  });

  it('SSR_RENDER_DATA secUid 优先于陈旧缓存与会话项对端链接', async () => {
    vi.stubGlobal('chrome', { storage: { local: { get() {}, set() {} } } });
    const { getAccountId } = await import('../src/channels/douyin.js');
    const ssrSecUid = 'MS4wLjABAAAAizL-ZXZsdIxqxP65DyHdUJnw7j7ogvrnFTJA8yhlKP-indxPGCYyd2ooE4v0b2DO';
    // 陈旧/错误缓存（真机实证值：指向对端用户）
    localStorage.setItem('hivebridge:account:douyin', 'MS4wLjABAAAABBUWE3vvGspw8V3XnrK33HwpFUOgq5Ay33HSYfunpW4');
    // 会话项链接 = 对端用户（真机实证：/chat 页零「我的」链接，只有会话项链接）
    document.body.innerHTML = `
      <div class="conversationConversationListwrapper">
        <a href="//www.douyin.com/user/MS4wLjABAAAABBUWE3vvGspw8V3XnrK33HwpFUOgq5Ay33HSYfunpW4">会话A</a>
      </div>`;
    window.SSR_RENDER_DATA = { app: { user: { info: { secUid: ssrSecUid, uid: '2957051671885384', nickname: 'HiveMtk' } } } };
    const id = getAccountId();
    expect(id).toBe(ssrSecUid);
    // 缓存被正确值覆盖（不再是对端 secUid）
    expect(localStorage.getItem('hivebridge:account:douyin')).toBe(ssrSecUid);
    delete window.SSR_RENDER_DATA;
    vi.unstubAllGlobals();
  });

  it('SSR 缺失时仍走会话项链接兜底（旧行为不回退）', async () => {
    vi.stubGlobal('chrome', { storage: { local: { get() {}, set() {} } } });
    const { getAccountId } = await import('../src/channels/douyin.js');
    document.body.innerHTML = `
      <div class="conversationConversationListwrapper">
        <a href="//www.douyin.com/user/MS4wABC123">会话A</a>
      </div>`;
    expect(getAccountId()).toBe('MS4wABC123');
    vi.unstubAllGlobals();
  });
});
