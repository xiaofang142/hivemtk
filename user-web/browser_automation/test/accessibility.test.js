import { describe, it, expect, beforeEach } from 'vitest';
import { collectInteractiveNodes, assembleSnapshot, getRefSelector, resetRefs } from '../src/core/accessibility.js';

describe('accessibility snapshot', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    resetRefs();
  });

  it('收集可交互元素并生成 @eN refs', () => {
    document.body.innerHTML = `
      <button id="btn-login">登录</button>
      <input id="kw" placeholder="搜索" />
      <a id="lnk" href="/next">下一页</a>
      <div style="display:none"><button id="hidden">隐身</button></div>
    `;
    const collected = collectInteractiveNodes();
    // display:none 的按钮被过滤；jsdom 中 <a href> 与 button/input 均可见
    expect(collected.nodes.length).toBeGreaterThanOrEqual(3);
    expect(collected.nodes.some((n) => n.name === '隐身')).toBe(false);
    const snap = assembleSnapshot(collected);
    expect(snap.count).toBe(collected.nodes.length);
    expect(snap.text).toContain('@e1');
    expect(snap.text).toContain('登录');
    // refs → selector 映射可用（首个 ref 指向某个真实元素）
    expect(getRefSelector('@e1')).toBeTruthy();
    expect(getRefSelector('@e9')).toBeNull();
  });

  it('resetRefs 后旧 ref 失效', () => {
    document.body.innerHTML = '<button id="a">A</button>';
    assembleSnapshot(collectInteractiveNodes());
    expect(getRefSelector('@e1')).toBe('#a');
    resetRefs();
    expect(getRefSelector('@e1')).toBeNull();
  });

  it('空页面快照为空', () => {
    const snap = assembleSnapshot(collectInteractiveNodes());
    expect(snap.count).toBe(0);
    expect(snap.text).toBe('');
  });
});
