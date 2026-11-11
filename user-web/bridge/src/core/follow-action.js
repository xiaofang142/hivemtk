// follow-action.js —— followUser 渠道 hook 的通用工厂（M-6，2026-11-11）

import { humanDelay } from './humanize.js';

//
// 职责：给定渠道的按钮候选定位与点击原语，产出 BaseAdapter.hooks.followUser ——
// 在当前打开的会话里找「关注/回关」按钮并模拟点击。判定口径全部 fixture 化：
//   - 可点击（未关注）文案与已关注文案分开成词表，命中已关注态绝不点击（关注按钮
//     在部分平台是 toggle，已关注态再点= 取消关注，不可逆）；
//   - 词表精确匹配（trim 后全等或单条正则），不做子串宽松匹配——宁可不点，不可点错；
//   - 真实 DOM 形态（按钮容器/文案词表）未经真机校准，选择器为 fixture 口径，
//     上线前必须先在真机校准词表并保持默认开关关闭。
//
// 本工厂不做任何限速/开关判定——那些在 BaseAdapter.followUser 统一收口
// （复用 rateLimiter 拟人节奏 + 会话冷却 + 默认关），这里只负责 DOM 动作。

// 未关注态（可点击）文案词表：trim 后小写全等匹配（支持带前后缀装饰符的候选）
const FOLLOW_ACTIONABLE_TEXTS = Object.freeze([
  '关注', '+ 关注', '+关注', '关注一下', '回关', '加关注',
  'follow', '+ follow', 'follow back', 'follow back?',
]);

// 已关注态词表：命中任意一条 ⇒ 绝不点击
const FOLLOW_FOLLOWED_TEXTS = Object.freeze([
  '已关注', '互相关注', '已互相关注', '取消关注', '已添加',
  'following', 'following back', 'friends', 'friend', 'pending', 'requested', 'unfollow',
]);

function normalizeBtnText(el) {
  try {
    return String(el.textContent || '').trim().toLowerCase();
  } catch (_) {
    return '';
  }
}

function isActionableFollowText(t) {
  return FOLLOW_ACTIONABLE_TEXTS.includes(t);
}
function isFollowedText(t) {
  return FOLLOW_FOLLOWED_TEXTS.includes(t);
}

// findFollowButton 在候选元素里按词表分类：返回 { action, followed } 两类首个命中。
function classifyCandidates(candidates) {
  let action = null;
  let followed = null;
  for (const el of candidates || []) {
    if (!el || !el.offsetParent) continue; // 不可见元素不参与
    const t = normalizeBtnText(el);
    if (!t) continue;
    if (!action && isActionableFollowText(t)) action = el;
    if (!followed && isFollowedText(t)) followed = el;
    if (action) break;
  }
  return { action, followed };
}

/**
 * createFollowUserHook 产出渠道 followUser hook。
 *
 * @param {object} cfg
 * @param {string} cfg.channelKey 渠道编码（日志用）
 * @param {string} cfg.channelLabel 渠道显示名（日志用）
 * @param {Function} cfg.findCandidates 返回当前会话页面上候选按钮元素数组
 * @param {Function} cfg.clickFn 模拟点击原语（各渠道统一传 core/dom.js 的 simulateRealClick）
 * @param {Function} [cfg.delayFn] 拟人延迟（默认 humanize.humanDelay('click')）
 * @param {object} cfg.log logger 实例
 * @returns {Function} async followUser(opts) => {ok, skipped?, error?}
 */
export function createFollowUserHook({ channelKey, channelLabel, findCandidates, clickFn, delayFn, log }) {
  if (typeof findCandidates !== 'function' || typeof clickFn !== 'function') {
    throw new Error('createFollowUserHook: findCandidates/clickFn required');
  }
  // 默认拟人延迟：与发送路径同一 humanize 参数源（click 档），不另建节奏通道。
  const delay = delayFn || ((opts) => humanDelay('click', opts || {}));
  return async function followUser(_opts = {}) {
    const { action, followed } = classifyCandidates(safeCandidates(findCandidates));
    if (!action) {
      if (followed) {
        log.info(`[${channelLabel}] followUser: 当前会话已关注（命中已关注态词表），跳过点击`);
        return { ok: true, skipped: 'already_followed', via: 'follow_user' };
      }
      log.warn(`[${channelLabel}] followUser: 未找到可点击的关注按钮（词表未命中，真机未校准或页面形态不符）`);
      return { ok: false, error: 'follow_button_not_found', via: 'follow_user' };
    }
    try {
      await delay({ channel: channelKey });
    } catch (_) { /* 拟人延迟失败不阻断动作 */ }
    try {
      clickFn(action);
    } catch (err) {
      log.warn(`[${channelLabel}] followUser: 点击异常`, err && err.message);
      return { ok: false, error: 'click_error', via: 'follow_user' };
    }
    log.info(`[${channelLabel}] followUser: 已模拟点击关注按钮`);
    return { ok: true, via: 'follow_user' };
  };
}

function safeCandidates(fn) {
  try {
    const out = fn();
    return Array.isArray(out) ? out : [];
  } catch (_) {
    return [];
  }
}
