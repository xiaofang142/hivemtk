import { describe, it, expect, vi, beforeEach } from 'vitest';
import { escapeHTML, sanitizeForDisplay, safeSetValue, safeSetContent, stripNonBmpChars } from '../src/core/sanitize.js';

describe('sanitize / stripNonBmpChars（借鉴 ChatGPT-On-CS pinduoduo 实证）', () => {
  it('剥星形面字符（emoji 补对面/生僻扩展区），BMP 内内容与中文保留', () => {
    const s = '你好😀世界𠀀AB';
    expect(stripNonBmpChars(s)).toBe('你好世界AB');
  });

  it('null/undefined 归空串；纯 BMP 串原样返回', () => {
    expect(stripNonBmpChars(null)).toBe('');
    expect(stripNonBmpChars(undefined)).toBe('');
    expect(stripNonBmpChars('普通文本 ~!@#')).toBe('普通文本 ~!@#');
  });
});

describe('sanitize / escapeHTML', () => {
  it('转义所有 HTML 危险字符', () => {
    expect(escapeHTML('<script>alert(1)</script>')).toBe('&lt;script&gt;alert(1)&lt;&#x2F;script&gt;');
    expect(escapeHTML('"hi" & \'bye\'')).toContain('&quot;');
    expect(escapeHTML('"hi" & \'bye\'')).toContain('&#39;');
    expect(escapeHTML('`code`')).toContain('&#x60;');
  });
  it('null/undefined/数字 安全返回', () => {
    expect(escapeHTML(null)).toBe('');
    expect(escapeHTML(undefined)).toBe('');
    expect(escapeHTML(123)).toBe('123');
  });
  it('保留普通中文/标点', () => {
    expect(escapeHTML('你好，世界！')).toBe('你好，世界！');
  });
});

describe('sanitize / sanitizeForDisplay', () => {
  it('剥离 null 字节和控制字符，保留换行/制表符', () => {
    const input = 'A\u0000B\u0001C\nD\tE';
    expect(sanitizeForDisplay(input)).toBe('ABC\nD\tE');
  });
  it('超过 maxBytes 时截断', () => {
    const input = 'x'.repeat(5000);
    const out = sanitizeForDisplay(input, 100);
    expect(out.length).toBe(100);
  });
  it('null/undefined 安全返回空串', () => {
    expect(sanitizeForDisplay(null)).toBe('');
    expect(sanitizeForDisplay(undefined)).toBe('');
  });
});

describe('sanitize / safeSetValue', () => {
  it('用 value setter 写入并派发 input/change 事件', () => {
    const el = {
      tagName: 'TEXTAREA',
      value: 'old',
      dispatchEvent: vi.fn(),
    };
    Object.defineProperty(globalThis, 'HTMLTextAreaElement', {
      value: { prototype: { value: {} } },
      writable: true,
      configurable: true,
    });
    Object.defineProperty(window, 'HTMLTextAreaElement', {
      value: { prototype: { value: {} } },
      writable: true,
      configurable: true,
    });
    Object.defineProperty(window.HTMLTextAreaElement.prototype, 'value', {
      configurable: true,
      get: () => el.value,
      set: (v) => { el.value = v; },
    });
    safeSetValue(el, 'hello\u0000world');
    expect(el.value).toBe('helloworld');
    expect(el.dispatchEvent).toHaveBeenCalled();
  });
  it('超长输入被截断到 4KB', () => {
    const el = {
      tagName: 'TEXTAREA',
      value: '',
      dispatchEvent: vi.fn(),
    };
    Object.defineProperty(window, 'HTMLTextAreaElement', {
      value: { prototype: { value: {} } },
      writable: true,
      configurable: true,
    });
    Object.defineProperty(window.HTMLTextAreaElement.prototype, 'value', {
      configurable: true,
      get: () => el.value,
      set: (v) => { el.value = v; },
    });
    safeSetValue(el, 'x'.repeat(5000));
    expect(el.value.length).toBe(4 * 1024);
  });
});

describe('sanitize / safeSetContent', () => {
  it('用 textContent 写入（避免 innerHTML XSS）并派发 input 事件', () => {
    const el = {
      textContent: 'old',
      dispatchEvent: vi.fn(),
    };
    // 监视 textContent 的赋值（用 defineProperty 跟踪）
    let assigned = null;
    Object.defineProperty(el, 'textContent', {
      configurable: true,
      get: () => assigned,
      set: (v) => { assigned = v; },
    });
    safeSetContent(el, '<img src=x onerror=alert(1)>');
    expect(assigned).toBe('<img src=x onerror=alert(1)>');
    expect(el.dispatchEvent).toHaveBeenCalled();
  });
  it('null 节点静默忽略', () => {
    expect(() => safeSetContent(null, 'x')).not.toThrow();
  });
});

