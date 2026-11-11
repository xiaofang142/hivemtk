// M-6（2026-11-11）：新用户（非关注好友）识别信号——词表、打标与 extra 透传。
// 验收锚点：
//   · 识别信号可上报（system 文案命中/tab 归属 → extra.new_user_unfollowed）；
//   · 无误伤（非命中文案不打标；打标只发生在已判 system 的分支）；
//   · system 归类现状不变（msg_type/sender_type 语义不受打标影响）；
//   · extra 经 makeUnifiedMessage 透传上行帧，且不覆盖调用方显式键（只加字段）。
import { describe, it, expect, beforeEach } from 'vitest';

import {
  FOLLOW_SIGNAL_KEY,
  FOLLOW_SIGNAL_SOURCE_KEY,
  FOLLOW_SIGNAL_SOURCES,
  followSignalFromSystemText,
  applyFollowSignal,
  followSignalOf,
} from '../src/core/follow-signal.js';
import { buildDouyinAdapter } from '../src/channels/douyin.js';
import { buildXianyuAdapter } from '../src/channels/xianyu.js';
import { buildTiktokAdapter } from '../src/channels/tiktok.js';
import { buildBilibiliAdapter, activeTabIsUnfollowed } from '../src/channels/bilibili.js';

function installComputedStyle() {
  if (typeof window !== 'undefined' && !window.getComputedStyle) {
    window.getComputedStyle = () => ({ textAlign: 'left', justifyContent: 'flex-start' });
  }
}

describe('follow-signal / followSignalFromSystemText 词表', () => {
  beforeEach(installComputedStyle);

  it('douyin：互相关注 → mutual_follow；关注了你 → peer_followed_you；已添加好友 → friend_request', () => {
    expect(followSignalFromSystemText('douyin', '你们已互相关注，现在可以发私信啦')).toBe(FOLLOW_SIGNAL_SOURCES.MUTUAL);
    expect(followSignalFromSystemText('douyin', '他关注了你')).toBe(FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW);
    expect(followSignalFromSystemText('douyin', '对方已添加你为好友')).toBe(FOLLOW_SIGNAL_SOURCES.FRIEND_REQUEST);
  });

  it('douyin：「互相关注后」（闲鱼式提示）不命中 mutual（排除后缀）', () => {
    expect(followSignalFromSystemText('douyin', '互相关注后才能发送消息')).toBe(null);
  });

  it('tiktok：followed you → peer；follow each other → not_mutual；accepted request → mutual', () => {
    expect(followSignalFromSystemText('tiktok', 'User123 followed you')).toBe(FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW);
    expect(followSignalFromSystemText('tiktok', 'Follow each other to start chatting')).toBe(FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL);
    expect(followSignalFromSystemText('tiktok', 'You are not friends anymore')).toBe(FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL);
    expect(followSignalFromSystemText('tiktok', 'User123 accepted your follow request')).toBe(FOLLOW_SIGNAL_SOURCES.MUTUAL);
    expect(followSignalFromSystemText('tiktok', 'User123 sent you a friend request')).toBe(FOLLOW_SIGNAL_SOURCES.FRIEND_REQUEST);
  });

  it('xianyu：你还不是对方好友 / 互相关注后 / 请勿向陌生人 → not_mutual', () => {
    expect(followSignalFromSystemText('xianyu', '你还不是对方好友，关注后可继续聊天')).toBe(FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL);
    expect(followSignalFromSystemText('xianyu', '互相关注后才能发送消息')).toBe(FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL);
    expect(followSignalFromSystemText('xianyu', '请勿向陌生人转账')).toBe(FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL);
  });

  it('无误伤：typing/撤回/普通文本不命中任何渠道词表', () => {
    expect(followSignalFromSystemText('douyin', '对方正在输入')).toBe(null);
    expect(followSignalFromSystemText('douyin', '对方撤回了一条消息')).toBe(null);
    expect(followSignalFromSystemText('tiktok', 'typing...')).toBe(null);
    expect(followSignalFromSystemText('xianyu', '消息已发出，但被对方拒收了')).toBe(null);
    expect(followSignalFromSystemText('douyin', '')).toBe(null);
    expect(followSignalFromSystemText('douyin', null)).toBe(null);
    expect(followSignalFromSystemText('bilibili', '任意文本')).toBe(null);
    expect(followSignalFromSystemText('weibo', '互相关注')).toBe(null);
  });
});

describe('follow-signal / applyFollowSignal 打标', () => {
  it('命中 → extra 打标（new_user_unfollowed=true + new_user_signal=来源）', () => {
    const parsed = { msg_type: 'system', text: '他关注了你' };
    applyFollowSignal(parsed, FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW);
    expect(parsed.extra[FOLLOW_SIGNAL_KEY]).toBe(true);
    expect(parsed.extra[FOLLOW_SIGNAL_SOURCE_KEY]).toBe(FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW);
    expect(followSignalOf(parsed)).toBe(FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW);
  });

  it('未命中（null）→ no-op，不产生 extra', () => {
    const parsed = { msg_type: 'system', text: '撤回了一条消息' };
    applyFollowSignal(parsed, null);
    expect(parsed.extra).toBeUndefined();
    expect(followSignalOf(parsed)).toBe(null);
  });

  it('只加字段：parsed 既有 extra 键不被覆盖', () => {
    const parsed = { msg_type: 'system', text: '互相关注', extra: { dm_target: 'member' } };
    applyFollowSignal(parsed, FOLLOW_SIGNAL_SOURCES.MUTUAL);
    expect(parsed.extra.dm_target).toBe('member');
    expect(parsed.extra[FOLLOW_SIGNAL_KEY]).toBe(true);
  });
});

describe('follow-signal / 渠道 parseMessageItem 集成（system 归类现状不变）', () => {
  beforeEach(() => {
    installComputedStyle();
    document.body.innerHTML = '';
  });

  function sysEl(text, cls) {
    const el = document.createElement('div');
    el.className = cls || 'message-item';
    el.textContent = text;
    return el;
  }

  it('douyin：互相关注系统消息 → system 归类不变 + extra 打标', () => {
    const a = buildDouyinAdapter();
    const item = sysEl('你们已互相关注', 'system-msg');
    const parsed = a.parseMessageItem(item);
    expect(parsed.msg_type).toBe('system');
    expect(parsed.sender_type).toBe('system');
    expect(parsed.extra && parsed.extra[FOLLOW_SIGNAL_KEY]).toBe(true);
    expect(parsed.extra[FOLLOW_SIGNAL_SOURCE_KEY]).toBe(FOLLOW_SIGNAL_SOURCES.MUTUAL);
  });

  it('douyin：普通系统消息（撤回）→ system 归类不变且不打标（无误伤）', () => {
    const a = buildDouyinAdapter();
    const item = sysEl('对方撤回了一条消息', 'system-msg');
    const parsed = a.parseMessageItem(item);
    expect(parsed.msg_type).toBe('system');
    expect(parsed.extra).toBeUndefined();
  });

  it('xianyu：你还不是对方好友 → system 归类不变 + not_mutual 打标', () => {
    const a = buildXianyuAdapter();
    const item = sysEl('你还不是对方好友，互相关注后即可聊天', 'system-msg');
    const parsed = a.parseMessageItem(item);
    expect(parsed.msg_type).toBe('system');
    expect(parsed.extra && parsed.extra[FOLLOW_SIGNAL_KEY]).toBe(true);
    expect(parsed.extra[FOLLOW_SIGNAL_SOURCE_KEY]).toBe(FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL);
  });

  it('tiktok：followed you → system 归类不变 + peer 打标', () => {
    const a = buildTiktokAdapter();
    const item = sysEl('user123 followed you', 'system-msg');
    const parsed = a.parseMessageItem(item);
    expect(parsed.msg_type).toBe('system');
    expect(parsed.extra && parsed.extra[FOLLOW_SIGNAL_SOURCE_KEY]).toBe(FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW);
  });
});

describe('follow-signal / bilibili 未关注人消息 tab 归属', () => {
  beforeEach(() => {
    installComputedStyle();
    document.body.innerHTML = '';
  });

  it('激活态命中（class 含 active）→ 消息打 unfollowed_tab 标（含客户消息）', () => {
    document.body.innerHTML = `
      <div class="tabs">
        <div class="tab" title="我的消息">我的消息</div>
        <div class="tab active" title="未关注人消息">未关注人消息</div>
      </div>
      <div class="message-list"><div id="m1" class="msg-item"><span class="message-content">你好</span></div></div>`;
    expect(activeTabIsUnfollowed()).toBe(true);
    const a = buildBilibiliAdapter();
    const item = document.getElementById('m1');
    const parsed = a.parseMessageItem(item);
    expect(parsed.sender_type).not.toBe('system');
    expect(parsed.extra && parsed.extra[FOLLOW_SIGNAL_KEY]).toBe(true);
    expect(parsed.extra[FOLLOW_SIGNAL_SOURCE_KEY]).toBe(FOLLOW_SIGNAL_SOURCES.TAB);
  });

  it('未激活/选择器不存在 → 不打标（fail-safe 宁缺勿错）', () => {
    document.body.innerHTML = `
      <div class="tabs">
        <div class="tab active" title="我的消息">我的消息</div>
        <div class="tab" title="未关注人消息">未关注人消息</div>
      </div>
      <div class="message-list"><div id="m2" class="msg-item"><span class="message-content">你好</span></div></div>`;
    expect(activeTabIsUnfollowed()).toBe(false);
    const a = buildBilibiliAdapter();
    const parsed = a.parseMessageItem(document.getElementById('m2'));
    expect(parsed.extra).toBeUndefined();
  });
});

describe('follow-signal / adapter extra 透传（makeUnifiedMessage 帧形状）', () => {
  beforeEach(() => {
    installComputedStyle();
    document.body.innerHTML = '';
  });

  it('_emitMessage：parsed.extra 随帧透传；调用方显式键冲突时优先（只加字段不改语义）', async () => {
    const a = buildDouyinAdapter();
    const seen = [];
    a.start({ onMessage: (m) => seen.push(m) });
    a.getConversationId = () => 'conv-signal';
    a.getAccountId = () => 'acc-signal';
    a._emitMessage(
      { sender_type: 'system', text: '他关注了你', msg_type: 'system', message_id: 'mh:aa11bb22', timestamp: 1, extra: { [FOLLOW_SIGNAL_KEY]: true, [FOLLOW_SIGNAL_SOURCE_KEY]: FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW } },
      { send_verified: true, [FOLLOW_SIGNAL_KEY]: true }
    );
    expect(seen).toHaveLength(1);
    const msg = seen[0];
    expect(msg.msg_type).toBe('system');
    expect(msg.extra[FOLLOW_SIGNAL_KEY]).toBe(true);
    expect(msg.extra[FOLLOW_SIGNAL_SOURCE_KEY]).toBe(FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW);
    // 调用方显式 extra（send_verified 结论位）不被 parsed.extra 挤掉
    expect(msg.extra.send_verified).toBe(true);
  });

  it('_emitPatrolMessage：批次内任一条带标记 → 随巡检帧透传', async () => {
    const a = buildDouyinAdapter();
    const seen = [];
    a.start({ onMessage: (m) => seen.push(m) });
    a._emitPatrolMessage('conv-patrol', { name: '客户' }, [
      { sender_type: 'customer', text: '在吗', msg_type: 'text', message_id: 'mh:p1', timestamp: 1 },
      { sender_type: 'system', text: '你还不是对方好友', msg_type: 'system', message_id: 'mh:p2', timestamp: 2, extra: { [FOLLOW_SIGNAL_KEY]: true, [FOLLOW_SIGNAL_SOURCE_KEY]: FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL } },
    ]);
    expect(seen).toHaveLength(1);
    expect(seen[0].extra && seen[0].extra[FOLLOW_SIGNAL_KEY]).toBe(true);
    expect(seen[0].extra[FOLLOW_SIGNAL_SOURCE_KEY]).toBe(FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL);
  });

  it('无标记消息的帧不携带 extra（与既有帧形状逐字节一致）', async () => {
    const a = buildDouyinAdapter();
    const seen = [];
    a.start({ onMessage: (m) => seen.push(m) });
    a._emitMessage({ sender_type: 'customer', text: '普通消息', msg_type: 'text', message_id: 'mh:p3', timestamp: 1 });
    expect(seen).toHaveLength(1);
    expect(seen[0].extra).toBeUndefined();
  });
});
