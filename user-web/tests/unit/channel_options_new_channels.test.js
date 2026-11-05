import { describe, it, expect } from 'vitest';
import {
  CHANNEL_OPTIONS,
  CHANNEL_LABEL_MAP,
  PLATFORM_GROUP_MEMBERS,
  getChannelLabel,
} from '../../src/constants/channel.js';

// 2026-11 六渠道统一接待：前端渠道表必须覆盖后端新渠道，
// 且公众号入站码 wechat 要显示成「微信公众号」而不是裸码。
describe('渠道表：2026-11 新增渠道与公众号别名', () => {
  const values = CHANNEL_OPTIONS.map((o) => o.value);

  it('新增京东/B站/微博三渠道，分组为 social', () => {
    for (const ch of ['jd', 'bilibili', 'weibo']) {
      expect(values).toContain(ch);
      const opt = CHANNEL_OPTIONS.find((o) => o.value === ch);
      expect(opt.group).toBe('social');
      expect(opt.label).toBeTruthy();
    }
  });

  it('公众号入站码 wechat 归一为「微信公众号」', () => {
    expect(getChannelLabel('wechat')).toBe('微信公众号');
    expect(getChannelLabel('weixin')).toBe('微信公众号');
  });

  it('新渠道别名归一为中文标签', () => {
    expect(getChannelLabel('jd_web')).toBe('京东');
    expect(getChannelLabel('bili')).toBe('B站');
    expect(getChannelLabel('wb')).toBe('微博');
    expect(getChannelLabel('wx_personal')).toBe('个人微信');
  });

  it('平台别名分组覆盖新渠道', () => {
    expect(PLATFORM_GROUP_MEMBERS.jd).toContain('jd_web');
    expect(PLATFORM_GROUP_MEMBERS.bilibili).toContain('bili');
    expect(PLATFORM_GROUP_MEMBERS.weibo).toContain('wb');
    expect(PLATFORM_GROUP_MEMBERS.personal_wx).toContain('wx_personal');
  });

  it('中文标签表包含新渠道', () => {
    expect(CHANNEL_LABEL_MAP.jd).toBe('京东');
    expect(CHANNEL_LABEL_MAP.bilibili).toBe('B站');
    expect(CHANNEL_LABEL_MAP.weibo).toBe('微博');
  });
});
