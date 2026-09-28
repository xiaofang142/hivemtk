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

// 步骤产出展平：result 是 action 返回值（snapshot/extract 的 JSON），按「键: 值」拉平成一行文本。
// 不用 JSON.stringify 直出：它会给字符串里的引号加反斜杠，用户读到的是 \" 而不是原文；
// 拉平后的值仍逐字保留，转义仍由出口统一做。
function flatValue(v, depth = 0) {
  if (v === null || v === undefined) return '';
  if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') return String(v);
  if (depth > 2) return '';
  if (Array.isArray(v)) return v.map((x) => flatValue(x, depth + 1)).filter((x) => x !== '').join(', ');
  return Object.entries(v).map(([k, x]) => `${k}: ${flatValue(x, depth + 1)}`).join('; ');
}

const STEP_TEXT_MAX = 300;

function clip(s) {
  return s.length > STEP_TEXT_MAX ? `${s.slice(0, STEP_TEXT_MAX)}…` : s;
}

function durationText(ms) {
  const v = Number(ms);
  if (!Number.isFinite(v) || v <= 0) return '';
  return v >= 10000 ? `${Math.round(v / 1000)}s` : `${(v / 1000).toFixed(1)}s`;
}

export function monitorBodyHtml(sess, steps) {
  const head = `<b>${escapeHtml(sess.status)}</b>` +
    (sess.error_msg ? ` · ${escapeHtml(sess.error_msg)}` : '');
  const rows = (steps || []).map((s) => {
    const bits = [
      `<span class="tag ${STATUS_TAG[s.status] || ''}">${escapeHtml(s.status)}</span>`,
    ];
    const dur = durationText(s.duration_ms);
    if (dur) bits.push(escapeHtml(dur));
    // 写台账态与「这一步是不可逆写」必须同屏：status 记跑成什么样，submit_state 记
    // 提交是否可能发生，两者在「send 到达但回查未见」时必然分叉
    if (s.is_write) bits.push(`写·${escapeHtml(s.submit_state || '未记')}`);
    const out = clip(flatValue(s.result));
    const err = s.error_msg ? clip(String(s.error_msg)) : '';
    return `${escapeHtml(s.step_index + 1)}. ${escapeHtml(s.action)} ${bits.join(' ')}<br/>` +
      (out ? `<span class="mon-out">产出 ${escapeHtml(out)}</span><br/>` : '') +
      (err ? `<span class="mon-err">原因 ${escapeHtml(err)}</span><br/>` : '');
  });
  return head + '<br/>' + rows.join('<br/>');
}

// Host 读数：count>0 只证明 WS 注册在场，能不能干活要看 servable（服务端的应用面回包证据）。
// 把「已连接」写成「在线」等于让用户按一个不成立的读数去点执行。
export function servableHostCount(st) {
  const count = Number(st?.count) || 0;
  if (!count) return 0;
  const hosts = Array.isArray(st?.hosts) ? st.hosts : [];
  if (hosts.length) return hosts.filter((h) => h?.online && h?.servable).length;
  // 没有明细时退回顶层汇总（普通用户视角可能只给 count/servable）
  return st?.servable ? count : 0;
}

export function hostStatusText(st) {
  const count = Number(st?.count) || 0;
  if (!count) return 'Host 离线：请确认本机 NM Host 已安装（~/.hivemtk/nm_host.conf 有 token）';
  const servable = servableHostCount(st);
  return servable
    ? `Host 已连接（${count} 台，${servable} 台可服务）`
    : `Host 已连接但不可服务（${count} 台都没有命令回包证据，点执行会失败）`;
}
