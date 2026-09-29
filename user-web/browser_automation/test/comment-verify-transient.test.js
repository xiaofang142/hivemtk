// comment_verify 的「零提交假绿」闸门。
//
// 背景（e2e 靶站实测，见 e2e/run.mjs toast_pollution_risk 场景）：verify 的容器选择器
// 不命中时会退到全文搜索兜底（容器类名可能变，兜底是必要的）。但全文搜索会把
// **非评论区的复读文案**也算命中——服务端拒收、评论根本没落库，浮层一句「真好吃」
// 就把 verified 判成 true。不可逆动作的自检出绿是最坏的一类假：据此认为评论已发出。
//
// 三组用例分别锁三条防线：
//  1. 真命中（容器里有已渲染评论）必须放行——修复不能把兜底杀成漏报
//  2. 瞬态浮层复读正文不得命中（本次修复）
//  3. 草稿仍留在输入框不得命中（既有防线，顺手钉住）
import { describe, it, expect } from 'vitest';
import { readFile } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';
import { parse } from 'acorn';

const PRIMITIVES = resolve(dirname(new URL(import.meta.url).pathname), '../src/core/primitives.js');

// 与 inject-sandbox.deserializedAsPage 同边界：源码串 → new Function（全局作用域，无闭包）
async function extractVerify() {
  const src = await readFile(PRIMITIVES, 'utf8');
  const ast = parse(src, { ecmaVersion: 'latest', sourceType: 'module' });
  const node = ast.body.find((n) => n.type === 'FunctionDeclaration' && n.id.name === 'injPostCommentVerify');
  expect(node, 'injPostCommentVerify 不在 primitives.js 顶层').toBeTruthy();
  return new Function(`return (${src.slice(node.start, node.end)})`)();
}

const COMMENT = '真好吃';
const OPTS = { timeoutMs: 100, itemSelector: '.note-comment-item' };

function freshDoc(bodyHtml) {
  document.body.innerHTML = bodyHtml;
}

// 评论列表 + 编辑器（编辑器就在容器里 = primitives.js:605 记录的真机小红书形态）
const box = (inner = '') =>
  `<div class="comments-container"><div class="comments-el">${inner}</div>` +
  `<div class="comment-post-box"><div class="content-textarea" contenteditable="true"></div>` +
  `<button>发送</button></div></div>`;

const item = (t) => `<div class="note-comment-item"><span>@我</span>${t}</div>`;

describe('comment_verify 不许在评论未落库时报绿', () => {
  it('评论已渲染进评论区：放行（兜底修复不得退化成漏报）', async () => {
    const verify = await extractVerify();
    freshDoc(box(item(COMMENT)));
    await expect(Promise.resolve(verify(COMMENT, OPTS))).resolves.toMatchObject({ ok: true, posted: true, verified: true });
  });

  it('评论未落库但浮层复读正文：必须报未发布', async () => {
    const verify = await extractVerify();
    freshDoc(box() + `<div class="toast">${COMMENT}</div>`);
    await expect(Promise.resolve(verify(COMMENT, OPTS))).resolves.toMatchObject({ posted: false, verified: false });
  });

  it('浮层用 role=status/alert 复读正文（无 toast 类名）：同样不许命中', async () => {
    const verify = await extractVerify();
    freshDoc(box() + `<div role="status">已复制：${COMMENT}</div>`);
    await expect(Promise.resolve(verify(COMMENT, OPTS))).resolves.toMatchObject({ posted: false, verified: false });
  });

  it('草稿仍留在输入框（从未提交）：必须报未发布', async () => {
    const verify = await extractVerify();
    freshDoc(box());
    document.querySelector('.content-textarea').textContent = COMMENT;
    await expect(Promise.resolve(verify(COMMENT, OPTS))).resolves.toMatchObject({ posted: false, verified: false });
  });
});

// 静态闸门：新增的剔除谓词必须真的接进两处 walker，否则上面的行为用例退化成空转
it('inTransientNode 已接入 textOutsideInputsIncludes 与 textExcludingInputs 两处文本累加', async () => {
  const src = await readFile(PRIMITIVES, 'utf8');
  const uses = src.split('inInputNode(node.parentElement)').length - 1;
  const guarded = src.split('inInputNode(node.parentElement) || inTransientNode(node.parentElement)').length - 1;
  expect(uses).toBeGreaterThan(0);
  expect(guarded).toBe(uses);
});
