import { describe, it, expect, beforeEach } from 'vitest';
import { collectInteractiveNodes, assembleSnapshot, getRefSelector, resetRefs, resetSnapshotBaseline } from '../src/core/accessibility.js';

describe('accessibility snapshot', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
    resetRefs();
    resetSnapshotBaseline(); // F6：全量清基线，用例间互不污染
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

  // 上限常量的声明位置在批14 从模块顶层挪进了注入函数体（见 collectInteractiveNodes 内注释）。
  // 这条用例是那个常量的唯一行为证据：挪错地方/漏掉就是这里先红。
  it('节点上限 400：超限页面只带回前 400 个，nodes 与 paths 同步截断', () => {
    document.body.innerHTML = Array.from({ length: 450 },
      (_, i) => `<button id="b${i}">按钮${i}</button>`).join('');
    const collected = collectInteractiveNodes();
    expect(collected.nodes.length).toBe(400);
    expect(collected.paths.length).toBe(400);
  });

  // ---- F6 新元素标记（browser-use *[index] 语义）----
  it('F6 首帧不打标；下一帧新出现的 role|name 行首带 *', () => {
    const key = 'tab-f6';
    resetSnapshotBaseline(key);
    document.body.innerHTML = '<button id="a">A</button>';
    const first = assembleSnapshot(collectInteractiveNodes(), key);
    // 首帧无基线 → 全部正常行，不打 *
    expect(first.text).not.toContain('*');
    expect(first.new_count).toBe(0);
    // 第二帧新增一个按钮：新行带 *，旧行不带
    document.body.innerHTML += '<button id="b">B</button>';
    const second = assembleSnapshot(collectInteractiveNodes(), key);
    const lines = second.text.split('\n');
    expect(lines.some((l) => l.startsWith('*') && l.includes('"B"'))).toBe(true);
    expect(lines.some((l) => !l.startsWith('*') && l.includes('"A"'))).toBe(true);
    expect(second.new_count).toBe(1);
  });

  it('F6 导航清基线：resetSnapshotBaseline 后首帧重新不打标', () => {
    const key = 'tab-nav';
    resetSnapshotBaseline(key);
    document.body.innerHTML = '<button id="a">A</button>';
    assembleSnapshot(collectInteractiveNodes(), key);
    document.body.innerHTML = '<button id="c">C</button>';
    resetSnapshotBaseline(key); // 模拟导航
    const after = assembleSnapshot(collectInteractiveNodes(), key);
    expect(after.text).not.toContain('*');
  });
});
