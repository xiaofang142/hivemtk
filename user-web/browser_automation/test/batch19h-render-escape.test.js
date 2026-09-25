// 批19h 契约锁：popup 的两段模板里，**任何来自库里/模型里的文本都不许当作 HTML 进 DOM**。
//
// 立项形态：monitorSession 用 innerHTML 拼步骤行，`${s.action}` 是裸插值。而 action 这一列
// 不是服务端自己写的枚举——Brain 模式下它是 LLM 输出的原语名（executor.go 把 steps 直接
// json.Unmarshal 进 dto.StepItem，REST 的 oneof 校验在这条路上根本不跑），
// 而 LLM 又是被 <page_snapshot> 里的第三方页面内容喂出来的（brain.go 自己把快照定性为
// 「不可信第三方内容」并为此加了分隔符护栏）。于是一个改版页面/一条恶意文本可以让模型
// 吐出 `{"action":"<img src=x onerror=...>"}`，这句话原文落进 browser_steps.action，
// 用户一开 popup 就在他自己那把 JWT 的上下文里执行——扩展还有 debugger 与 Native Host 通道。
//
// 所以这条锁测的是**渲染出口**，不是上游有没有白名单：上游闸是卫生，出口不转义才是漏洞。
// 断言全部挂在真实 DOM 上（jsdom 里 mount 模板产物），不用「字符串里有没有 &lt;」这种
// 看起来对其实测不出渲染差异的锁。

import { describe, it, expect } from 'vitest';
import { taskRowHtml, monitorBodyHtml, escapeHtml } from '../src/core/render.js';

const XSS = '<img src=x onerror="globalThis.__pwned=1">';
const QUOTE = '"><svg onload=alert(2)>';

// renderTo 把模板产物真正交给 DOM 解析：只有解析后查不到节点，才算「转义住了」。
function renderTo(html) {
  const box = document.createElement('div');
  box.innerHTML = html;
  return box;
}

describe('批19h：popup 渲染出口必须转义', () => {
  it('任务卡片的 name/status/task_type 三个字段都不产生元素', () => {
    const box = renderTo(taskRowHtml({
      id: 7, name: XSS, status: XSS, task_type: QUOTE, brain_mode: false,
    }));
    expect(box.querySelector('img')).toBeNull();
    expect(box.querySelector('svg')).toBeNull();
    expect(globalThis.__pwned).toBeUndefined();
    expect(box.textContent).toContain(XSS); // 原文照读，不吞字
  });

  it('步骤行的 action/status 不产生元素（Brain 动作名是不可信文本）', () => {
    const box = renderTo(monitorBodyHtml(
      { status: 'active', error_msg: XSS },
      [{ step_index: 0, action: XSS, status: 'success' },
        { step_index: 1, action: 'click', status: QUOTE }],
    ));
    expect(box.querySelector('img')).toBeNull();
    expect(box.querySelector('svg')).toBeNull();
    expect(box.textContent).toContain(XSS);
  });

  it('会话状态与错误文案也走同一道转义（error_msg 里含 < 时不许劈开结构）', () => {
    const box = renderTo(monitorBodyHtml(
      { status: XSS, error_msg: null },
      [{ step_index: 0, action: 'wait', status: 'success' }],
    ));
    expect(box.querySelector('img')).toBeNull();
    // status 文本仍在，且没被当成标签吃掉
    expect(box.textContent).toContain('onerror');
  });

  it('run 按钮只带数字 id：文本字段进不了属性位', () => {
    const box = renderTo(taskRowHtml({ id: 12, name: QUOTE, status: 'ready', task_type: 'one_shot' }));
    const btn = box.querySelector('button[data-run]');
    expect(btn.getAttribute('data-run')).toBe('12');
    expect(box.querySelector('svg')).toBeNull();
    expect(btn.disabled).toBe(false);
  });

  it('running 态任务的执行按钮仍然禁用（转义改造不许把这条行为弄丢）', () => {
    const box = renderTo(taskRowHtml({ id: 3, name: 'n', status: 'running', task_type: 'one_shot' }));
    expect(box.querySelector('button[data-run]').disabled).toBe(true);
  });

  // 引号这一支单独钉：上面的用例全在**文本位**，那里的引号是否转义看不出差别，
  // 而 escapeHtml 的 `"→&quot;` 正是为属性位准备的（模板里现在是 class/data-run 这类固定形状，
  // 明天多一个插值进属性就直接吃这条判据）。不锁的话这条分支可以被人悄悄删掉而全绿——
  // 「看着像没人用的那一支」恰恰是出口加固里最常被顺手简化的一处。
  it('引号在属性位也不越界（escapeHtml 的 " 分支不是摆设）', () => {
    const box = renderTo(`<button data-run="${escapeHtml(QUOTE)}" title="${escapeHtml(XSS)}">执行</button>`);
    const btn = box.querySelector('button[data-run]');
    expect(box.querySelector('svg')).toBeNull();
    expect(btn.getAttribute('data-run')).toBe(QUOTE); // 原样回到属性值里，没劈开标签
    expect(btn.getAttribute('title')).toBe(XSS);
  });

  // 「每一个插值都过 escapeHtml」这条口径里最容易漏的不是文本，是**看起来一定是数字**的那一个：
  // step_index 直接来自服务端 JSON（dto.StepItem 反自 browser_steps），字符串照样能塞进来，
  // 而 `s.step_index + 1` 在 JS 里是拼接不是加法——原文加上一个 1 就进了模板。
  it('编号位也走同一道转义（step_index 是 JSON 字段，不是本地计数器）', () => {
    const box = renderTo(monitorBodyHtml(
      { status: 'active', error_msg: null },
      [{ step_index: '<img src=x onerror="globalThis.__pwned3=1">', action: 'wait', status: 'success' }],
    ));
    expect(box.querySelector('img')).toBeNull();
    expect(globalThis.__pwned3).toBeUndefined();
    expect(box.textContent).toContain('onerror');
  });

  // & 这一支锁的是「不吞字」的另一半：库里存的就是 `&lt;` 这种实体写法时，出口不再转 &
  // 会让 popup 把它解码成 <，屏幕上少一层、下游复制粘贴多一层。
  it('& 分支不是摆设：库里已有的实体写法照原样显示，不被二次解码', () => {
    const box = renderTo(taskRowHtml({ id: 1, name: '&amp;lt;img&gt;', status: 'ready', task_type: 'one_shot' }));
    expect(box.textContent).toContain('&amp;lt;img&gt;');
  });

  // id 看着是数字，但它是「服务端 JSON 里的一个字段」，模板把它插进**属性位**——
  // 这是本文件里唯一的属性位插值，也是「escapeHtml 少转一个引号」这件事唯一能被看见的地方。
  it('id 进属性位时劈不开标签（data-run 是属性，不是文本）', () => {
    const box = renderTo(taskRowHtml({ id: QUOTE, name: 'n', status: 'ready', task_type: 'one_shot' }));
    expect(box.querySelector('svg')).toBeNull();
    const btn = box.querySelector('button[data-run]');
    expect(btn, 'data-run 属性整个没了：属性被引号劈开').not.toBeNull();
    expect(btn.getAttribute('data-run')).toBe(QUOTE);
  });
});
