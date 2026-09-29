// run.mjs — 浏览器自动化模块「评论链」真浏览器端到端验证台。
//
// 与单测的区别（为什么还需要它）：
//  单测用 jsdom + strictExecuteScript mock 覆盖 dispatch 分层逻辑，但**没有布局引擎**，
//  可点性三件套（zero_box / covered / disabled）全靠造出来的几何跑分支。
//  这里用真 Chromium + 真实选择器 + 真 trusted 鼠标点击，验证的是注入函数的
//  「序列化边界 + 真 DOM 命中 + 视口坐标落点 + MutationObserver 回渲染」这一整段。
//
// 选择器靶站：e2e/target.html 复刻小红书/抖音评论区 DOM（编辑器就在
//  .comments-container 里面 = primitives.js:605 注释里那条真机形态）。
//
// 用法：node e2e/run.mjs
import { createServer } from 'node:http';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { dirname, join, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parse } from 'acorn';
import { chromium } from 'playwright';

const HERE = dirname(fileURLToPath(import.meta.url));
const MOD = join(HERE, '..', 'src', 'core', 'primitives.js');
const REPORT_DIR = join(HERE, 'report');

const MIME = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript' };

// ---- 1. 按源码串取出注入函数（模拟 func.toString() 送到页面那一份） ----
async function extractInjectFns() {
  const src = await readFile(MOD, 'utf8');
  const ast = parse(src, { ecmaVersion: 'latest', sourceType: 'module' });
  const wanted = ['injPostCommentPrep', 'injPostCommentSend', 'injPostCommentVerify'];
  const out = {};
  for (const node of ast.body) {
    if (node.type === 'FunctionDeclaration' && wanted.includes(node.id.name)) {
      out[node.id.name] = src.slice(node.start, node.end);
    }
  }
  const missing = wanted.filter((n) => !out[n]);
  if (missing.length) throw new Error('注入函数提取失败: ' + missing.join(','));
  return out;
}

// ---- 2. 本地靶站静态服务 ----
async function serve() {
  const server = createServer(async (req, res) => {
    const url = new URL(req.url, 'http://x');
    const file = join(HERE, url.pathname === '/' ? 'target.html' : url.pathname);
    try {
      const body = await readFile(file);
      res.writeHead(200, { 'content-type': MIME[extname(file)] || 'application/octet-stream' });
      res.end(body);
    } catch {
      res.writeHead(404); res.end('not found');
    }
  });
  await new Promise((r) => server.listen(0, '127.0.0.1', r));
  return { server, base: `http://127.0.0.1:${server.address().port}` };
}

// ---- 3. 页面侧执行器：与 Chrome executeScript 同边界（源码串 → new Function） ----
async function evalInject(page, src, args) {
  const t0 = Date.now();
  const result = await page.evaluate(
    async ({ src, args }) => {
      const fn = new Function(`return (${src})`)(); // 无闭包：引用模块自由变量会 ReferenceError
      return await fn(...args);
    },
    { src, args }
  );
  return { result, ms: Date.now() - t0 };
}

const step = (name, ok, detail, ms) => ({ name, ok, detail, ms });

// ---- 4. 场景 ----
async function runHappyPath(page, base, fns, { app, label, text }) {
  const steps = [];
  const shot = (n) => `${REPORT_DIR}/${label}-${n}.png`;
  await page.goto(`${base}/target.html?app=${app}&case=ok`);
  await page.waitForFunction(() => window.__targetReady === true);
  steps.push(step('load', true, await page.evaluate(() => window.__targetInfo)));

  // 阶段一 prep
  const prep = await evalInject(page, fns.injPostCommentPrep, [text, '']);
  steps.push(step('comment_prep', prep.result.ok === true, prep.result, prep.ms));
  if (prep.result.ok !== true) return { steps, ok: false };
  await page.screenshot({ path: shot('1-prep') });

  // contenteditable（React 受控形态）：合成注入无效，由 trusted 键入兜底
  if (prep.result.needs_trusted) {
    const t0 = Date.now();
    await page.keyboard.type(text, { delay: 20 });
    const typed = await page.evaluate(() => {
      const el = document.querySelector('.content-textarea, textarea');
      return (el.isContentEditable ? el.innerText : el.value).trim();
    });
    steps.push(step('trusted_type', typed === text, { typed, expect: text }, Date.now() - t0));
    if (typed !== text) return { steps, ok: false };
    await page.screenshot({ path: shot('2-typed') });
  }

  // 阶段二 send：算按钮视口坐标
  const send = await evalInject(page, fns.injPostCommentSend, ['', '']);
  steps.push(step('comment_send_locate', send.result.ok === true, send.result, send.ms));
  if (send.result.ok !== true) return { steps, ok: false };

  // 真 trusted 落点点击（等价 CDP Input.dispatchMouseEvent 的 mousePressed/released）
  const tClick = Date.now();
  await page.mouse.click(send.result.x, send.result.y);
  steps.push(step('trusted_click', true, { x: send.result.x, y: send.result.y,
    radius: send.result.jitter_radius, selector: send.result.selector }, Date.now() - tClick));
  await page.screenshot({ path: shot('3-clicked') });

  // 阶段三 verify：MutationObserver 等回渲染
  const verify = await evalInject(page, fns.injPostCommentVerify,
    [text, { timeoutMs: 6000, containerSelector: '', itemSelector: '.note-comment-item' }]);
  steps.push(step('comment_verify', verify.result.verified === true, verify.result, verify.ms));
  await page.screenshot({ path: shot('4-verified') });

  // 交叉核对：DOM 里确实多了一条，且草稿已清空（防「草稿仍在框里」假绿）。
  // 回渲染有真实延迟（站侧 ~300ms），必须轮询而不是立即断言。
  const tDom = Date.now();
  let dom = null;
  while (Date.now() - tDom < 4000) {
    dom = await page.evaluate(() => {
      const items = [...document.querySelectorAll('.note-comment-item')];
      const el = document.querySelector('.content-textarea, textarea');
      return { count: items.length, last: (items[items.length - 1] || {}).innerText,
               draftLeft: (el.isContentEditable ? el.innerText : el.value).trim() };
    });
    if (dom.count === 3 && dom.draftLeft === '' && (dom.last || '').includes(text)) break;
    await page.waitForTimeout(100);
  }
  steps.push(step('dom_crosscheck',
    dom.count === 3 && dom.draftLeft === '' && (dom.last || '').includes(text), dom));
  return { steps, ok: steps.every((s) => s.ok) };
}

// 闸门场景：该拦的必须拦下（不可逆动作不能盲发）
async function runGate(page, base, fns, { c, text }) {
  await page.goto(`${base}/target.html?app=xhs&case=${c}`);
  await page.waitForFunction(() => window.__targetReady === true);
  await evalInject(page, fns.injPostCommentPrep, [text, '']);
  const send = await evalInject(page, fns.injPostCommentSend, ['', '']);
  const submitted = await page.evaluate(() =>
    document.querySelectorAll('.note-comment-item').length); // 点击从未发生
  return { case: c, send_result: send.result,
           blocked: send.result.ok !== true, no_side_effect: submitted === 2 };
}

// 假绿防线：只把文字塞进输入框、从未提交 → verify 必须报未发布
async function runNoFalseGreen(page, base, fns) {
  const text = '真好吃';
  await page.goto(`${base}/target.html?app=xhs&case=ok`);
  await page.waitForFunction(() => window.__targetReady === true);
  await evalInject(page, fns.injPostCommentPrep, [text, '']);
  const verify = await evalInject(page, fns.injPostCommentVerify,
    [text, { timeoutMs: 1500, containerSelector: '', itemSelector: '.note-comment-item' }]);
  return { verify_result: verify.result,
           guard_ok: verify.result.verified !== true && verify.result.posted !== true };
}

// 风险场景：评论未落库，但页面出现含目标文本的浮层 → verify 全文兜底是否会假绿
async function runToastRisk(page, base, fns) {
  const text = '真好吃';
  await page.goto(`${base}/target.html?app=xhs&case=toastrisk`);
  await page.waitForFunction(() => window.__targetReady === true);
  await evalInject(page, fns.injPostCommentPrep, [text, '']);
  await page.keyboard.type(text, { delay: 10 });
  const send = await evalInject(page, fns.injPostCommentSend, ['', '']);
  if (send.result.ok) await page.mouse.click(send.result.x, send.result.y);
  const verify = await evalInject(page, fns.injPostCommentVerify,
    [text, { timeoutMs: 1500, containerSelector: '', itemSelector: '.note-comment-item' }]);
  const items = await page.evaluate(() => document.querySelectorAll('.note-comment-item').length);
  return { ok: verify.result.verified !== true,   // 正确表现 = 不假绿
           comment_count: items,                  // 仍为 2 = 未落库
           verify_result: verify.result,
           note: '评论未落库，浮层文案含目标文本：若 verified=true 即全文兜底误命中' };
}

const main = async () => {
  await mkdir(REPORT_DIR, { recursive: true });
  const fns = await extractInjectFns();
  const { server, base } = await serve();
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 560, height: 900 } });
  const report = { generatedAt: new Date().toISOString(), scenarios: [] };

  const scenarios = [
    { app: 'xhs', label: 'xhs', text: '真好吃' },
    { app: 'dy', label: 'dy', text: '真好吃' },
  ];
  for (const s of scenarios) {
    const r = await runHappyPath(page, base, fns, s);
    report.scenarios.push({ name: `happy_${s.label}`, ...r });
  }
  for (const c of ['nobutton', 'disabled', 'covered']) {
    report.scenarios.push({ name: `gate_${c}`, ...(await runGate(page, base, fns, { c, text: '真好吃' })) });
  }
  report.scenarios.push({ name: 'no_false_green', ...(await runNoFalseGreen(page, base, fns)) });
  report.scenarios.push({ name: 'toast_pollution_risk', ...(await runToastRisk(page, base, fns)) });

  await browser.close();
  server.close();

  const passOf = (s) => (typeof s.ok === 'boolean' ? s.ok
    : typeof s.guard_ok === 'boolean' ? s.guard_ok
      : Boolean(s.blocked && s.no_side_effect));
  report.passed = report.scenarios.every(passOf);
  await writeFile(join(REPORT_DIR, 'report.json'), JSON.stringify(report, null, 2));

  for (const s of report.scenarios) {
    console.log(`\n■ ${s.name}  ${passOf(s) ? 'PASS' : 'FAIL'}`);
    if (s.steps) for (const st of s.steps) {
      console.log(`   ${st.ok ? '✓' : '✗'} ${st.name}  ${st.ms ?? '-'}ms  ${JSON.stringify(st.detail)}`);
    }
    if (s.send_result) console.log(`   ${s.blocked ? '✓' : '✗'} blocked=${s.blocked} ${JSON.stringify(s.send_result)}`);
    if (s.verify_result) console.log(`   ${passOf(s) ? '✓' : '✗'} ${JSON.stringify(s.verify_result)}`);
    if (s.note) console.log(`   · ${s.note}`);
  }
  console.log(`\n汇总: ${report.passed ? 'ALL PASS' : 'HAS FAIL'} → ${join(REPORT_DIR, 'report.json')}`);
  process.exit(report.passed ? 0 : 1);
};

main().catch((e) => { console.error(e); process.exit(1); });
