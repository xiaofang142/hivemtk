import { describe, it, expect } from 'vitest';
import { setValue } from '../src/core/dom.js';

// 2026-11-13 bili prod 实证：.msb-textarea 等 DIV 占位壳命中 findInputEl，
// setValue 对 DIV 调 HTMLInputElement.prototype.value setter 抛 Illegal invocation
// → 下行回写失败 20 次 exhaust（mh:a825b262）。守卫：DIV 不走原生 setter。
describe('setValue tag guard', () => {
  it('INPUT 走原生 value setter', () => {
    const el = document.createElement('input');
    setValue(el, 'hi');
    expect(el.value).toBe('hi');
  });

  it('TEXTAREA 走原生 value setter', () => {
    const el = document.createElement('textarea');
    setValue(el, 'hi');
    expect(el.value).toBe('hi');
  });

  it('DIV 占位壳属性赋值兜底，不抛 Illegal invocation', () => {
    const el = document.createElement('div');
    expect(() => setValue(el, 'hi')).not.toThrow();
    expect(el.value).toBe('hi');
  });
});
