import { describe, it, expect, beforeEach, vi } from 'vitest';

vi.mock('../src/core/humanize.js', () => ({
  humanDelay: async () => {},
}));

import {
  buildPddAdapter, getAccountId, getConversationId, getConversationList, isPddHost,
} from '../src/channels/pdd.js';
import { SENDER, CHANNELS } from '../src/core/types.js';
import { EXPERIMENTAL_CHANNELS } from '../src/core/constants.js';

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

// B7 fake 契约夹具：pdd 商家聊天页形态（A 源选择器形态回读，16a 真机校准前为
// 「fake 契约」档——钉的是 adapter 行为契约，不宣称真 DOM 已校准）。
function fixture() {
  document.body.innerHTML = `
    <div class="chat-list">
      <div class="chat-portrait active" data-id="10001"><span class="remark">买家甲</span><i class="badge">2</i></div>
      <div class="chat-portrait" data-id="10002"><span class="remark">买家乙</span></div>
    </div>
    <div class="base-info"><div class="remark-row"><span>买家甲</span></div></div>
    <div class="msg-list">
      <div class="onemsg" data-id="m1"><div class="msg-content"><span>你好，有货吗</span></div></div>
      <div class="onemsg cs-item" data-id="m2"><div class="msg-content"><span>有的</span></div></div>
      <div class="onemsg system-msg" data-id="m3"><div class="system-msg-inner">订单已支付</div></div>
      <div class="onemsg" data-id="m4"><div class="msg-content image-msg"><img src="https://img.pinduoduo.com/a.jpg" /></div></div>
      <div class="onemsg" data-id="m5"><div class="msg-content good-card">商品卡内容</div></div>
    </div>
    <textarea id="replyTextarea"></textarea>
    <div class="reply-footer"><button class="send-btn">发送</button></div>
  `;
}

describe('pdd adapter（CH-16 首切片，实验渠道）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://mms.pinduoduo.com/chat-merchant/index.html');
    fixture();
  });

  // —— 注册/词表契约（B7 fake 契约集：与后端注册表同一身份）——

  it('canonical 渠道名与后端 model.ChannelPDD 一致（pdd）', () => {
    expect(CHANNELS.PDD).toBe('pdd');
    // Experimental 双侧 parity 由 Go registry_parity_test 钉；这里自证扩展侧登记在案。
    expect(EXPERIMENTAL_CHANNELS.pdd).toBe(true);
    expect(EXPERIMENTAL_CHANNELS[CHANNELS.PDD]).toBe(true);
  });

  it('host 路由：pinduoduo.com 命中，其他域不命中', () => {
    expect(isPddHost()).toBe(true);
    stubLocation('https://mms.pinduoduo.com/other');
    expect(isPddHost()).toBe(true);
    stubLocation('https://foo.example.com/');
    expect(isPddHost()).toBe(false);
  });

  it('match 要求会话列表 + 输入框同时存在（商家后台搜索框不当聊天框）', () => {
    const a = buildPddAdapter();
    expect(a.match()).toBe(true);
    // 摘掉输入框：只剩会话列表 → 不匹配（京麦式严格判定同源）。
    document.getElementById('replyTextarea').remove();
    expect(a.match()).toBe(false);
  });

  // —— 会话身份 / 店铺隔离契约 ——

  it('getConversationId 取活动会话 data-id（新 ID 路由：平台会话 ID 即定位键）', () => {
    expect(getConversationId()).toBe('10001');
    const a = buildPddAdapter();
    expect(a.getConversationId()).toBe('10001');
  });

  it('getConversationList 返回全部会话且不串（多子账号同名访客由 ID 区分）', () => {
    document.querySelectorAll('.chat-portrait')[1].innerHTML =
      '<span class="remark">买家甲</span>'; // 与会话 1 同名不同 ID
    const list = getConversationList();
    expect(list.length).toBe(2);
    expect(list[0].id).toBe('10001');
    expect(list[1].id).toBe('10002');
    expect(list[1].name).toBe('买家甲');
    // 同名不同 ID：路由键不同（多子账号同名访客不串的 adapter 侧契约）。
    expect(list[0].id).not.toBe(list[1].id);
  });

  it('无会话列表时 getConversationId 不猜测返回 null（无店铺 ID 拒启）', () => {
    document.querySelector('.chat-list').remove();
    document.querySelector('.base-info').remove();
    expect(getConversationId()).toBe(null);
  });

  // —— 入站解析契约（方向/系统/媒体/卡片）——

  it('parseMessageItem 解析文本且统一 customer 占位（自/他判定移交后端）', () => {
    const a = buildPddAdapter();
    const items = a.getMessageItems();
    expect(items.length).toBe(5);
    const parsed = a.parseMessageItem(items[0]);
    expect(parsed.text).toBe('你好，有货吗');
    expect(parsed.sender_type).toBe(SENDER.CUSTOMER);
    expect(parsed.message_id).toBe('m1');
  });

  it('system-msg 上报为 system 类型不冒充买家发言', () => {
    const a = buildPddAdapter();
    const items = a.getMessageItems();
    const parsed = a.parseMessageItem(items[2]);
    expect(parsed).not.toBeNull();
    expect(parsed.msg_type).toBe('system');
    expect(parsed.sender_type).toBe(SENDER.SYSTEM);
    expect(parsed.text).toContain('订单已支付');
  });

  it('图片消息上报 media_url + image 类型，不把 URL 拼成文本', () => {
    const a = buildPddAdapter();
    const items = a.getMessageItems();
    const parsed = a.parseMessageItem(items[3]);
    expect(parsed.msg_type).toBe('image');
    expect(parsed.media_url).toContain('img.pinduoduo.com');
    expect(parsed.text).toBe('[图片]');
  });

  it('商品卡消息保留占位降级不可发送（未知卡片不拼成用户文本）', () => {
    const a = buildPddAdapter();
    const items = a.getMessageItems();
    const parsed = a.parseMessageItem(items[4]);
    // 卡片走占位：msg_type 标注 + 占位文案，绝不把卡片内容拼成买家文本。
    expect(parsed.msg_type).toBe('card');
    expect(parsed.text).not.toContain('商品卡内容');
    expect(parsed.sender_type).toBe(SENDER.CUSTOMER);
  });

  // —— 出站契约（发送/漂移/回车兜底）——

  it('sendText 写入输入框并点击发送，发送后清空', async () => {
    const a = buildPddAdapter();
    const input = document.getElementById('replyTextarea');
    const btn = document.querySelector('.send-btn');
    let seen = '';
    btn.addEventListener('click', () => { seen = input.value; });
    await a.hooks.sendText('拼多多回复', {});
    expect(seen).toBe('拼多多回复');
    expect(input.value).toBe('');
  });

  it('sendText 会话漂移：assertConv 返回 false 时清空输入框并抛 conv-drift（错会话拒发）', async () => {
    const a = buildPddAdapter();
    const input = document.getElementById('replyTextarea');
    const btn = document.querySelector('.send-btn');
    let clicked = 0;
    btn.addEventListener('click', () => { clicked += 1; });
    await expect(a.hooks.sendText('漂移文案', {
      assertConv: () => false,
    })).rejects.toMatchObject({ code: 'conv-drift' });
    // 拒发：按钮从未被点、输入框已清空（不留半截草稿给下一个人）。
    expect(clicked).toBe(0);
    expect(input.value).toBe('');
  });

  it('sendText 无发送按钮时回车兜底（A 源即回车兜底）', async () => {
    const a = buildPddAdapter();
    document.querySelector('.send-btn').remove();
    const input = document.getElementById('replyTextarea');
    let enter = 0;
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') enter += 1; });
    await a.hooks.sendText('回车发送', {});
    expect(enter).toBe(1);
    expect(input.value).toBe('');
  });

  // —— 账号 ID 契约（店铺 ID 缺失时稳定占位，不猜测）——

  it('getAccountId 无登录信息时返回稳定占位 pdd-unknown', () => {
    expect(getAccountId()).toBe('pdd-unknown');
    // 稳定性：重复调用同值（不因 DOM 无关变化漂移）。
    expect(getAccountId()).toBe('pdd-unknown');
  });

  it('getAccountId 命中 meta 商店信息时透传', () => {
    const meta = document.createElement('meta');
    meta.setAttribute('name', 'mallid');
    meta.setAttribute('content', 'mall-888');
    document.head.appendChild(meta);
    expect(getAccountId()).toBe('mall-888');
    meta.remove();
  });
});

// M-4（2026-11-11）：pdd 评估结论 = 上行媒体解析已对齐 A 源证据（text/image/card），
// 「16d 专属实现前降级不可发送」注释属下行 sendMedia 口径（M-3 已录实验渠道保持文本降级），
// 本轮不新增分支；补 avatar 排除回归 + 语音缺口如实登记（A 源无语音形态证据）。
describe('M-4 pdd 上行解析对齐评估（不新增分支）', () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    stubLocation('https://mms.pinduoduo.com/chat-merchant/index.html');
    document.body.innerHTML = '';
  });

  it('回归：头像图不误判为消息图（文本+头像 → text + media_url 空）', () => {
    const a = buildPddAdapter();
    document.body.innerHTML = `
      <div class="msg-list">
        <div class="onemsg" data-id="m9"><div class="msg-content"><img class="avatar" src="https://img.pinduoduo.com/av.jpg">纯文本</div></div>
      </div>
      <textarea id="replyTextarea"></textarea>
      <div class="reply-footer"><button class="send-btn">发送</button></div>
    `;
    const items = a.getMessageItems();
    const p = a.parseMessageItem(items[0]);
    expect(p).not.toBeNull();
    expect(p.msg_type).toBe('text');
    expect(p.media_url).toBe('');
    expect(p.text).toBe('纯文本');
  });

  it('缺口登记：纯语音气泡当前整条丢弃（A 源无语音形态证据，随 BR-1 真机校准补 voice 分支）', () => {
    // 诚实钉住现状：pdd 解析器无 voice 分支，纯语音（无文本无图）过不了
    // 「!text && !mediaUrl → null」门禁。真机校准前不补齐 = 不凭空捏造选择器。
    const a = buildPddAdapter();
    document.body.innerHTML = `
      <div class="msg-list">
        <div class="onemsg" data-id="m10"><div class="msg-content"><div class="voice"></div></div></div>
      </div>
      <textarea id="replyTextarea"></textarea>
      <div class="reply-footer"><button class="send-btn">发送</button></div>
    `;
    const items = a.getMessageItems();
    expect(a.parseMessageItem(items[0])).toBeNull();
  });
});
