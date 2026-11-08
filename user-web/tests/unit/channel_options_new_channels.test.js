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

  // CH-16 首切片（2026-11-09）：拼多多商家聊天进前端渠道表，分组 social。
  it('CH-16 拼多多（pdd）进渠道表，social 分组，别名归一', () => {
    expect(values).toContain('pdd');
    const opt = CHANNEL_OPTIONS.find((o) => o.value === 'pdd');
    expect(opt.group).toBe('social');
    expect(opt.label).toBe('拼多多');
    expect(getChannelLabel('pdd_web')).toBe('拼多多');
    expect(getChannelLabel('pinduoduo')).toBe('拼多多');
    expect(PLATFORM_GROUP_MEMBERS.pdd).toEqual(['pdd', 'pdd_web', 'pinduoduo']);
    expect(CHANNEL_LABEL_MAP.pdd).toBe('拼多多');
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

// CS-07（2026-11-07）实验渠道等级必须以文字标注，不得仅靠 tag 颜色表达；
// 个人微信与公众号分格，伴生未交付要在提示面写明。
describe('渠道表：实验等级文字标注（CS-07）', () => {
  const optionByValue = (v) => CHANNEL_OPTIONS.find((o) => o.value === v);

  it('四个实验渠道的描述含「实验」文字', () => {
    for (const ch of ['kuaishou', 'jd', 'bilibili', 'weibo']) {
      const opt = optionByValue(ch);
      expect(opt, `${ch} 应存在`).toBeTruthy();
      expect(opt.description).toContain('实验');
    }
  });

  it('CH-16 拼多多实验标注：描述含「实验」与未校准声明', () => {
    const opt = optionByValue('pdd');
    expect(opt, 'pdd 应存在').toBeTruthy();
    expect(opt.description).toContain('实验');
    expect(opt.description).toContain('未经真机校准');
  });

  it('个人微信提示面写明伴生未交付，与公众号区分', () => {
    const pw = optionByValue('personal_wx');
    expect(pw.description).toContain('伴生未交付');
    const wx = optionByValue('weixin');
    expect(wx.label).toBe('微信公众号');
    expect(pw.label).toBe('个人微信');
  });

  it('正式桥接渠道描述不携带实验标注', () => {
    for (const ch of ['douyin', 'xiaohongshu', 'xianyu', 'tiktok']) {
      expect(optionByValue(ch).description).not.toContain('实验');
    }
  });
});
