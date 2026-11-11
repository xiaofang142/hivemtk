// M-6（2026-11-11）：followUser 自动关注动作——adapter 统一闸门与渠道 hook 工厂。
// 验收锚点：
//   · 自动关注默认关闭（caps 未拉到/未开启 = 永不执行）；
//   · 有开关与风控节奏（复用 rateLimiter + 会话级关注冷却 + 让路下发）；
//   · 关注动作不阻塞 AI 回复主链路（让路 sendOutbound；失败不抛进 downlink 主流程）；
//   · 渠道 hook 词表：已关注态绝不点击（防 toggle 误取关）。
import { describe, it, expect, vi, beforeEach } from 'vitest';

import { BaseAdapter } from '../src/core/channel-adapter.js';
import { createFollowUserHook } from '../src/core/follow-action.js';
import { buildDouyinAdapter } from '../src/channels/douyin.js';

function makeAdapter({ hooks = {}, rateLimiter } = {}) {
  return new BaseAdapter({
    name: 'test',
    channel: 'douyin',
    SEL: {},
    hooks,
    rateLimiter: rateLimiter || null,
  });
}

describe('follow-action / adapter.followUser 闸门', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('默认关：未 setFollowUserEnabled 一律拒绝（skipped=disabled），hook 不被调用', async () => {
    const hook = vi.fn(async () => ({ ok: true }));
    const a = makeAdapter({ hooks: { followUser: hook } });
    const r = await a.followUser({ conversationId: 'c1' });
    expect(r.ok).toBe(false);
    expect(r.skipped).toBe('disabled');
    expect(hook).not.toHaveBeenCalled();
  });

  it('开关开但渠道 hooks 未实现 followUser → skipped=no-hook（xianyu 场景）', async () => {
    const a = makeAdapter({ hooks: {} });
    a.setFollowUserEnabled(true, ['douyin']);
    const r = await a.followUser({ conversationId: 'c1' });
    expect(r.skipped).toBe('no-hook');
  });

  it('渠道不在 caps 清单 → skipped=channel-not-allowed', async () => {
    const hook = vi.fn(async () => ({ ok: true }));
    const a = makeAdapter({ hooks: { followUser: hook } });
    a.setFollowUserEnabled(true, ['tiktok', 'bilibili']);
    const r = await a.followUser({ conversationId: 'c1' });
    expect(r.skipped).toBe('channel-not-allowed');
    expect(hook).not.toHaveBeenCalled();
  });

  it('caps 拉取失败（follow_user_channels 缺省）→ 视为未授权（fail-safe）', async () => {
    const hook = vi.fn(async () => ({ ok: true }));
    const a = makeAdapter({ hooks: { followUser: hook } });
    a.setFollowUserEnabled(true, []);
    const r = await a.followUser({ conversationId: 'c1' });
    expect(r.skipped).toBe('channel-not-allowed');
  });

  it('下发在途（isSendBusy）→ 让路跳过（skipped=busy），不阻塞主链路', async () => {
    const hook = vi.fn(async () => ({ ok: true }));
    const a = makeAdapter({ hooks: { followUser: hook, getConversationId: () => 'c1' } });
    a.setFollowUserEnabled(true, ['douyin']);
    a.beginOutbound(); // 模拟 AI 回复下发临界区
    const r = await a.followUser({ conversationId: 'c1' });
    a.endOutbound();
    expect(r.skipped).toBe('busy');
    expect(hook).not.toHaveBeenCalled();
  });

  it('成功路径：hook 调用、冷却表写入；冷却窗口内重复动作行 → skipped=cooldown（ok:true 收口）', async () => {
    const hook = vi.fn(async () => ({ ok: true }));
    // 零间隔限速桩：本用例专测冷却闸，不测拟人最小间隔（那是 rate-limiter 自身的职责）
    const rl = { cfg: { minIntervalMs: 0 }, tryAcquire: () => ({ allowed: true, reason: 'ok', retryAfterMs: 0, waitHintMs: 0 }), markSent: () => {} };
    const a = makeAdapter({ hooks: { followUser: hook, getConversationId: () => 'c1' }, rateLimiter: rl });
    a.setFollowUserEnabled(true, ['douyin']);
    const r1 = await a.followUser({ conversationId: 'c1' });
    expect(r1.ok).toBe(true);
    expect(hook).toHaveBeenCalledTimes(1);
    const r2 = await a.followUser({ conversationId: 'c1' });
    expect(r2.ok).toBe(true);
    expect(r2.skipped).toBe('cooldown');
    expect(hook).toHaveBeenCalledTimes(1); // 冷却期内不重复点击
    // 不同会话不受冷却影响
    a.getConversationId = () => 'c2';
    await a.followUser({ conversationId: 'c2' });
    expect(hook).toHaveBeenCalledTimes(2);
  });

  it('限速拒绝 → rateLimited（动作行留 pending 重推，与文本行一致）', async () => {
    const hook = vi.fn(async () => ({ ok: true }));
    const rl = {
      cfg: { minIntervalMs: 1500 },
      tryAcquire: vi.fn(() => ({ allowed: false, reason: 'account bucket(12/min)', retryAfterMs: 1000, waitHintMs: 0 })),
      markSent: vi.fn(),
    };
    const a = makeAdapter({ hooks: { followUser: hook, getConversationId: () => 'c1' }, rateLimiter: rl });
    a.setFollowUserEnabled(true, ['douyin']);
    const r = await a.followUser({ conversationId: 'c1' });
    expect(r.ok).toBe(false);
    expect(r.rateLimited).toBe(true);
    expect(hook).not.toHaveBeenCalled();
    expect(rl.markSent).not.toHaveBeenCalled();
  });

  it('成功后 markSent（与文本发送共享节奏配额）', async () => {
    const hook = vi.fn(async () => ({ ok: true }));
    const rl = {
      cfg: { minIntervalMs: 0 },
      tryAcquire: vi.fn(() => ({ allowed: true, reason: 'ok', retryAfterMs: 0, waitHintMs: 0 })),
      markSent: vi.fn(),
    };
    const a = makeAdapter({ hooks: { followUser: hook, getConversationId: () => 'c1' }, rateLimiter: rl });
    a.setFollowUserEnabled(true, ['douyin']);
    await a.followUser({ conversationId: 'c1' });
    expect(rl.tryAcquire).toHaveBeenCalledWith('douyin', '', 'c1', 'follow_user');
    expect(rl.markSent).toHaveBeenCalledWith('douyin', '', 'c1', 'follow_user');
  });

  it('hook 失败/抛错 → ok:false + error 留痕，不抛异常（不阻塞 downlink 主流程）', async () => {
    const hook = vi.fn(async () => ({ ok: false, error: 'follow_button_not_found' }));
    const a = makeAdapter({ hooks: { followUser: hook, getConversationId: () => 'c1' } });
    a.setFollowUserEnabled(true, ['douyin']);
    const r = await a.followUser({ conversationId: 'c1' });
    expect(r.ok).toBe(false);
    expect(r.error).toBe('follow_button_not_found');

    const throwing = makeAdapter({
      hooks: { followUser: async () => { throw new Error('boom'); }, getConversationId: () => 'c2' },
    });
    throwing.setFollowUserEnabled(true, ['douyin']);
    const r2 = await throwing.followUser({ conversationId: 'c2' });
    expect(r2.ok).toBe(false);
    expect(r2.error).toContain('hook_error');
  });

  it('目标非当前会话且 openConversation 失败 → notFound（downlink ack failed 终态）', async () => {
    const hook = vi.fn(async () => ({ ok: true }));
    const a = makeAdapter({ hooks: { followUser: hook, getConversationId: () => 'other' } });
    a.setFollowUserEnabled(true, ['douyin']);
    a.openConversation = vi.fn(async () => null);
    const r = await a.followUser({ conversationId: 'gone' });
    expect(r.ok).toBe(false);
    expect(r.notFound).toBe(true);
    expect(hook).not.toHaveBeenCalled();
  });

  it('conversationId 缺失 → notFound', async () => {
    const a = makeAdapter({ hooks: { followUser: async () => ({ ok: true }) } });
    a.setFollowUserEnabled(true, ['douyin']);
    const r = await a.followUser({});
    expect(r.notFound).toBe(true);
  });

  it('真实渠道 adapter：douyin hook 存在且默认关（build 出来未 set 前 followUser 拒绝执行）', async () => {
    const a = buildDouyinAdapter();
    expect(typeof a.followUser).toBe('function');
    expect(typeof a.hooks.followUser).toBe('function');
    const r = await a.followUser({ conversationId: 'c1' });
    expect(r.skipped).toBe('disabled');
  });
});

describe('follow-action / createFollowUserHook 渠道词表', () => {
  function btn(text, visible = true) {
    const el = {
      textContent: text,
      offsetParent: visible ? {} : null,
    };
    return el;
  }
  const log = { info: () => {}, warn: () => {}, error: () => {}, debug: () => {} };
  const click = vi.fn(() => {});
  const mk = (candidates) => createFollowUserHook({
    channelKey: 'douyin', channelLabel: '抖音',
    findCandidates: () => candidates,
    clickFn: click,
    delayFn: async () => {},
    log,
  });

  beforeEach(() => { click.mockClear(); });

  it('未关注态（关注/+关注/回关/Follow back）→ 点击成功', async () => {
    const r = await mk([btn('发消息'), btn('+ 关注'), btn('已关注')])({});
    expect(r.ok).toBe(true);
    expect(click).toHaveBeenCalledTimes(1);
  });

  it('英文词表（Follow / Follow back）→ 点击成功', async () => {
    const r = await mk([btn('Follow back')])({});
    expect(r.ok).toBe(true);
    expect(click).toHaveBeenCalledTimes(1);
  });

  it('只有已关注态 → 绝不点击（skipped=already_followed，防 toggle 误取关）', async () => {
    const r = await mk([btn('发消息'), btn('已关注'), btn('互相关注')])({});
    expect(r.ok).toBe(true);
    expect(r.skipped).toBe('already_followed');
    expect(click).not.toHaveBeenCalled();
  });

  it('已关注态在场时，即使排在可点击候选前也不误点（分类遍历按词表判）', async () => {
    // 「已关注」不是可点击词表成员；「关注」在后面 → 仍应点「关注」
    const r = await mk([btn('已关注'), btn('关注')])({});
    expect(r.ok).toBe(true);
    expect(click).toHaveBeenCalledTimes(1);
  });

  it('词表全不命中 → ok:false + follow_button_not_found（真机未校准的诚实失败）', async () => {
    const r = await mk([btn('发消息'), btn('私信')])({});
    expect(r.ok).toBe(false);
    expect(r.error).toBe('follow_button_not_found');
    expect(click).not.toHaveBeenCalled();
  });

  it('不可见候选（offsetParent=null）不参与分类', async () => {
    const r = await mk([btn('关注', false)])({});
    expect(r.ok).toBe(false);
    expect(r.error).toBe('follow_button_not_found');
  });

  it('findCandidates 抛错 → 安全失败（不向上抛）', async () => {
    const hook = createFollowUserHook({
      channelKey: 'douyin', channelLabel: '抖音',
      findCandidates: () => { throw new Error('dom gone'); },
      clickFn: click, delayFn: async () => {}, log,
    });
    const r = await hook({});
    expect(r.ok).toBe(false);
    expect(r.error).toBe('follow_button_not_found');
  });
});
