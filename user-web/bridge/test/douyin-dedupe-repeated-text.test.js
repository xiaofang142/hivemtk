import { describe, it, expect } from 'vitest';

import { dedupeRepeatedText } from '../src/channels/douyin.js';

// —— dedupeRepeatedText（2026-08-17 回环止血 + 2026-11-14 连接空白错位补口）——
// 生产实证（message_hub 租户 18 douyin inbound）：
//   「蜂桥下行手动L 732783 蜂桥下行手动L 732783」——双写以空白连接，总长
//   14+1+14=29，旧对半守卫 half=14 从连接空白起头错位 1 字节必不命中，
//   双写原样上行 → 服务端自回显精确匹配失败 → AI 回环风险。
describe('douyin dedupeRepeatedText', () => {
  it('空白连接的双写（奇数总长）→ 折叠为单份（连接空格错位补口）', () => {
    const t = '蜂桥下行手动L 732783';
    expect(dedupeRepeatedText(`${t} ${t}`)).toBe(t);
  });

  it('多空白连接的双写同样折叠', () => {
    const t = '这是需要折叠的重复消息内容AB12';
    expect(dedupeRepeatedText(`${t}  ${t}`)).toBe(t);
  });

  it('既有守卫不回归：无标点整段精确对半（偶数总长）仍折叠', () => {
    const t = '一段没有标点的重复文本块ABCDEF123456';
    expect(dedupeRepeatedText(t + t)).toBe(t);
  });

  it('不误杀：正常长文本（非双写）原样保留', () => {
    const t = '你好，我想咨询一下这款商品的价格、规格和发货时间，另外还有售后政策。';
    expect(dedupeRepeatedText(t)).toBe(t);
  });

  it('不误杀：去空白后不成对半的两段不同内容原样保留', () => {
    const t = '第一句是完全不同的内容甲乙丙丁，第二句讲的是另一件事戊己庚辛壬癸。';
    expect(dedupeRepeatedText(t)).toBe(t);
  });

  it('既有守卫不回归：按句去重（同句重复 N 次）仍生效', () => {
    const s = '重复的句子。';
    expect(dedupeRepeatedText(s + s + s)).toBe(s);
  });

  it('短文本（去空白 < 20 码点）不做对半折叠，避免误伤短句', () => {
    expect(dedupeRepeatedText('好的 好的')).toBe('好的 好的');
  });
});
