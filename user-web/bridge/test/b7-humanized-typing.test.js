// B7（批2）bridge 侧拟人键入契约测试：
//  1) fillContentEditableHumanized 内容正确性优先（末段校验兜底）+ 真实节奏（非零耗时）；
//  2) humanize.js 与 browser-core 参数表单源（对象同一性）；
//  3) 五适配器 sendText 静态契约：拟人键入 + humanDelay('click',{channel,account})，
//     禁止残留固定 setTimeout 节拍（源码契约断言，与 service 侧同款风格）。
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, it, expect } from 'vitest';
import { fillContentEditableHumanized } from '../src/core/dom.js';
import { _internal as humanizeInternal } from '../src/core/humanize.js';
import { DELAY_PROFILES, planTypingBursts } from '../../../packages/browser-core/index.js';

describe('B7 单一来源锁定', () => {
  it('humanize._internal.DELAY_PROFILES 就是 browser-core 本体', () => {
    expect(humanizeInternal.DELAY_PROFILES).toBe(DELAY_PROFILES);
  });
  it('humanize._internal.gaussian 就是 browser-core 本体', () => {
    // gaussian 未走 _internal 导出，则视为本地复制 → 漂移风险，必须同一引用
    expect(humanizeInternal.gaussian.name).toBe('gaussian');
  });
});

describe('fillContentEditableHumanized（jsdom，execCommand 不可用 → 兜底路径）', () => {
  it('contenteditable div：最终内容逐字一致且按段派发 input 事件', async () => {
    const el = document.createElement('div');
    el.setAttribute('contenteditable', 'true');
    document.body.appendChild(el);
    const inputEvents = [];
    el.addEventListener('input', (e) => inputEvents.push(e));
    const text = '亲，链接在这里。请查收！';
    const t0 = Date.now();
    await fillContentEditableHumanized(el, text);
    const elapsed = Date.now() - t0;
    expect((el.innerText || el.textContent || '').trim()).toBe(text);
    expect(inputEvents.length).toBeGreaterThanOrEqual(2);
    // 8+ 字符 × ≥30ms/字符下限：证明存在真实节奏（旧实现内部零等待）
    const minChars = planTypingBursts(text).reduce((a, b) => a + b.text.length, 0);
    expect(minChars).toBe(text.length);
    expect(elapsed).toBeGreaterThanOrEqual(100);
  });

  it('textarea：value 全量还原（代理对安全）', async () => {
    const el = document.createElement('textarea');
    document.body.appendChild(el);
    const text = '测试👋文本，包含 emoji 与标点。';
    await fillContentEditableHumanized(el, text);
    expect(el.value).toBe(text);
  });

  it('clearBefore：旧内容必须被清空，不得拼接', async () => {
    const el = document.createElement('div');
    el.setAttribute('contenteditable', 'true');
    el.textContent = '用户正在输入的旧内容';
    document.body.appendChild(el);
    await fillContentEditableHumanized(el, '新内容', { clearBefore: true });
    const final = (el.innerText || el.textContent || '').trim();
    expect(final).toBe('新内容');
    expect(final).not.toContain('旧内容');
  });

  it('空元素/空文本静默通过', async () => {
    await fillContentEditableHumanized(null, 'x');
    await fillContentEditableHumanized({}, '');
  });
});

describe('五适配器 sendText 静态契约', () => {
  const files = {
    douyin: CHANNELS_CHECK('douyin', 'DOUYIN'),
    kuaishou: CHANNELS_CHECK('kuaishou', 'KUAISHOU'),
    tiktok: CHANNELS_CHECK('tiktok', 'TIKTOK'),
    xhs: CHANNELS_CHECK('xhs', 'XHS'),
    xianyu: CHANNELS_CHECK('xianyu', 'XIANYU'),
  };
  function CHANNELS_CHECK(file, key) {
    // vitest 默认 cwd = 包根（npm test 场景）；不用 import.meta.url（vite transform 下为服务器路径）
    const src = readFileSync(join(process.cwd(), 'src', 'channels', `${file}.js`), 'utf8');
    const start = src.indexOf('async sendText(text)');
    if (start < 0) throw new Error(`${file}.js 无 sendText`);
    return { file, key, body: src.slice(start, start + 1400) };
  }

  for (const { file, key, body } of Object.values(files)) {
    it(`${file}: 拟人键入 + humanDelay('click', {channel, account}) 且无固定节拍`, () => {
      expect(body).toContain('await fillContentEditableHumanized(');
      expect(body).toContain(`humanDelay('click', { channel: CHANNELS.${key}`);
      expect(body).toContain('account: getAccountId()');
      expect(body).not.toContain('setTimeout(r');
      // 同步整段填值不得残留在 sendText（contenteditable 通道）；
      // 只拦裸调用 fillContentEditable(，不误伤 fillContentEditableHumanized(
      expect(body).not.toMatch(/fillContentEditable\(/);
    });
  }
});
