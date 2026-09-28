// popup 监控面板的读数完整性：跑完一步，用户得在 popup 里看见「拿到什么」和「为什么失败」。
//
// 走查实测到的形态：monitorBodyHtml 只出「序号 · action · status」三段。
// 于是一条 extract 步骤显示 success，用户看不到它到底提没提到内容；一条 click 显示 failed，
// 原因（error_msg）与耗时（duration_ms）都躺在同一行响应里却没渲染。
// 用户必须切到网页端监控页才能看懂 popup 里那几个绿灯——popup 就变成了「知道在跑」但
// 「不知道跑出什么」的进度条。
//
// 同时锁 Host 状态那句话：接口给了 servable/last_cmd_ok_at，popup 却按 count>0 写死「N 台在线」。
// 注册在场但一次都没回过包的 Host 正是「点执行就报 Host 未连接」的那种，
// 显示成「在线」等于让用户按一个不成立的读数去点执行。
//
// 断言全挂在真实 DOM 上（jsdom 解析模板产物），并保留转义口径：result/error_msg
// 同样是第三方页面能影响的文本，不许当作 HTML 进 DOM。

import { describe, it, expect } from 'vitest';
import { monitorBodyHtml, hostStatusText } from '../src/core/render.js';

const XSS = '<img src=x onerror="globalThis.__pwned=1">';

function renderTo(html) {
  const box = document.createElement('div');
  box.innerHTML = html;
  return box;
}

const step = (over = {}) => ({
  step_index: 0, action: 'extract', status: 'success', duration_ms: 1234, ...over,
});

describe('popup 步骤行要供出产出与失败原因', () => {
  it('步骤有 result：内容渲染出来，而不是只剩一个绿灯', () => {
    const box = renderTo(monitorBodyHtml(
      { status: 'completed', error_msg: null },
      [step({ result: { titles: ['机械键盘测评', '百元键盘推荐'] } })],
    ));
    expect(box.textContent).toContain('机械键盘测评');
    expect(box.textContent).toContain('百元键盘推荐');
  });

  it('步骤失败：error_msg 上屏，用户不必切到网页端才知道为什么', () => {
    const box = renderTo(monitorBodyHtml(
      { status: 'failed', error_msg: null },
      [step({ action: 'click', status: 'failed', error_msg: '元素在 10s 内未出现在页面上' })],
    ));
    expect(box.textContent).toContain('元素在 10s 内未出现在页面上');
  });

  it('耗时逐条给出：慢步骤与秒失败在 popup 里要能区分', () => {
    const box = renderTo(monitorBodyHtml(
      { status: 'completed', error_msg: null },
      [step({ duration_ms: 800 }), step({ step_index: 1, duration_ms: 12000 })],
    ));
    expect(box.textContent).toContain('0.8s');
    expect(box.textContent).toContain('12s');
  });

  it('没有产出/失败的步骤不许渲染出 undefined 或空壳', () => {
    const box = renderTo(monitorBodyHtml(
      { status: 'active', error_msg: null },
      [step({ result: null, error_msg: '' })],
    ));
    expect(box.textContent).not.toContain('undefined');
    expect(box.textContent).not.toContain('null');
  });

  it('result / error_msg 里的 HTML 不产生元素（出口转义口径延伸到新字段）', () => {
    const box = renderTo(monitorBodyHtml(
      { status: 'failed', error_msg: XSS },
      [step({ result: { raw: XSS }, error_msg: XSS })],
    ));
    expect(box.querySelector('img')).toBeNull();
    expect(globalThis.__pwned).toBeUndefined();
    expect(box.textContent).toContain(XSS); // 原文照读，不吞字
  });

  it('不可逆写步骤带台账态：prepared 与 verified 在 popup 里就该分得开', () => {
    const box = renderTo(monitorBodyHtml(
      { status: 'active', error_msg: null },
      [step({ action: 'post_comment', is_write: true, submit_state: 'verified' }),
        step({ step_index: 1, action: 'post_comment', is_write: true, submit_state: 'prepared' })],
    ));
    expect(box.textContent).toContain('写');
    expect(box.textContent).toContain('verified');
    expect(box.textContent).toContain('prepared');
  });
});

describe('popup 的 Host 状态读数', () => {
  it('已连接且有可服务的 Host：说出可服务', () => {
    const text = hostStatusText({ count: 1, servable: true, hosts: [{ online: true, servable: true }] });
    expect(text).toContain('可服务');
  });

  it('连接在场但不可服务：不许只报「在线」了事', () => {
    const text = hostStatusText({ count: 1, servable: false, hosts: [{ online: true, servable: false }] });
    expect(text).toContain('不可服务');
    expect(text).not.toContain('在线');
  });

  it('一台都没有：给出离线口径', () => {
    const text = hostStatusText({ count: 0, servable: false, hosts: [] });
    expect(text).toContain('离线');
  });

  it('接口没给 hosts 明细时按顶层 servable 说话（普通用户视角的字段子集）', () => {
    expect(hostStatusText({ count: 2, servable: true })).toContain('可服务');
    expect(hostStatusText({ count: 2, servable: false })).toContain('不可服务');
  });
});
