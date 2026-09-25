// render.js — popup 两段模板的 HTML 生成（纯函数，不碰 DOM）
// 从 popup/index.js 拆出来的理由只有一条：popup 模块顶层就抓节点、装监听器，
// 想在测试里锁「库里的文本不许当作 HTML 进 DOM」这一句话，就得把整张 popup.html
// 连同 chrome/fetch 全缝上。拆成纯函数后，判据可以直接喂数据、把产物交给 jsdom 解析来看。
//
// 口径：模板里除本模块自己写的标签外，**每一个插值都过 escapeHtml**，不按「这个字段看着像
// 枚举」豁免——status/task_type/action 现在由服务端写，明天就可能由 LLM 写（Brain 模式的
// action 就是模型原样输出，REST 的 oneof 在那条路上不跑）。转义是出口该做的事，不是上游的恩赐。

export const STATUS_TAG = { running: 'running', done: 'done', completed: 'done', failed: 'failed', ready: 'ready' };

export function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

export function taskRowHtml(t) {
  const tagCls = STATUS_TAG[t.status] || '';
  return `
        <div class="t-name">#${escapeHtml(t.id)} ${escapeHtml(t.name)}</div>
        <div class="t-meta">
          <span class="tag ${tagCls}">${escapeHtml(t.status)}</span>
          <span>${escapeHtml(t.task_type)}${t.brain_mode ? ' · Brain' : ''}</span>
          <button data-run="${escapeHtml(t.id)}" ${t.status === 'running' ? 'disabled' : ''}>执行</button>
        </div>`;
}

export function monitorBodyHtml(sess, steps) {
  return `<b>${escapeHtml(sess.status)}</b>` +
    (sess.error_msg ? ` · ${escapeHtml(sess.error_msg)}` : '') +
    '<br/>' +
    steps.map((s) => `${escapeHtml(s.step_index + 1)}. ${escapeHtml(s.action)} ` +
      `<span class="tag ${STATUS_TAG[s.status] || ''}">${escapeHtml(s.status)}</span>`).join('<br/>');
}
