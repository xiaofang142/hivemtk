// ego-browser 重载 Chrome 已加载扩展的共用模块。
//
// 为什么需要它：chrome://extensions 的「重新加载」按钮只能点，脚本化点它需要
// 一个浏览器自动化通道。ego-browser 复用用户登录态、独立 Space 不抢标签页，
// 是这里唯一合规的通道（见 AGENTS.md 浏览器规则）。
//
// 关键约束（踩过的坑，别改）：
//   1. ego-browser 的 nodejs 运行时会把子进程 console.log 直接透传到父终端，
//      execFileSync 的 stdout 捕获为空——状态必须写文件回传。
//   2. chrome://extensions 页面快照的 ref 会随页面变化失效，每次动作前重新
//      snapshot 取 ref。
//   3. 未过滤时页面可能有多个「重新加载」按钮（多个扩展卡片），必须先用搜索框
//      过滤到唯一卡片，要求按钮 ref 恰好 1 个再点。
//   4. 「加载未打包的扩展程序」点击后弹出的是 macOS 原生目录面板：CDP 合成
//      点击不带真实用户激活，Chrome 会拒绝弹出 file chooser（实测
//      waitForFileChooser 8s 超时），且原生面板不受 CDP 键盘事件控制。
//      因此"首次加载新目录"无法全自动，只能人工一次；日常版本更新走本模块
//      （目录已登记在 Chrome，rsync 覆盖 + 重新加载即可）。
import { execFileSync } from 'node:child_process';
import { existsSync, rmSync, readFileSync } from 'node:fs';

/**
 * 在 Chrome 里重新加载指定扩展，并读回快照中的版本号。
 *
 * @param {object} o
 * @param {string} o.name       扩展显示名（用于 chrome://extensions 搜索框过滤）
 * @param {string} [o.expectVersion] 期望版本；读到不一致时 ok=false
 * @param {string} [o.resultPath]    状态回传文件路径
 * @param {boolean} [o.dryRun]
 * @returns {{ok: boolean, version?: string|null, card?: string, step?: string, error?: string}}
 */
export function reloadExtension({ name, expectVersion, resultPath = '/tmp/hivebridge-reload-result.json', dryRun = false }) {
  if (existsSync(resultPath)) rmSync(resultPath);
  // 注意：这个脚本跑在 ego-browser 的 Node 运行时里，顶层可用 await。
  const script = `
const fs = require('node:fs');
const RESULT = ${JSON.stringify(resultPath)};
const NAME = ${JSON.stringify(name)};
const write = (o) => fs.writeFileSync(RESULT, JSON.stringify(o));
(async () => {
  try {
    const ts = await taskSpace('hivebridge-publish');
    const p = await ts.page('p1');
    await p.goto('chrome://extensions/');
    await p.waitForTimeout(1500);
    let snap = await p.snapshot({ scope: 'full_page' });
    let text = typeof snap === 'string' ? snap : JSON.stringify(snap);
    const boxRef = (text.match(/searchbox "搜索扩展程序" \\[ref=(\\d+)[,\\]]/) || [])[1];
    if (!boxRef) return write({ ok: false, step: 'searchbox' });
    await p.fill(boxRef, NAME);
    await p.waitForTimeout(700);
    snap = await p.snapshot({ scope: 'full_page' });
    text = typeof snap === 'string' ? snap : JSON.stringify(snap);
    const refs = [...text.matchAll(/button "重新加载" \\[ref=(\\d+)[,\\]]/g)].map((m) => m[1]);
    if (refs.length !== 1) return write({ ok: false, step: 'reload-button', count: refs.length });
    await p.click(refs[0]);
    await p.waitForTimeout(1800);
    snap = await p.snapshot({ scope: 'full_page' });
    text = typeof snap === 'string' ? snap : JSON.stringify(snap);
    const ver = (text.match(/\\b(\\d+\\.\\d+\\.\\d+)\\b/) || [])[1];
    const card = text.split('\\n').filter((l) => new RegExp(NAME.split(/[\\s—]/)[0]).test(l)).slice(0, 2).join(' | ');
    // MV3 重载后 content script 需页面重新加载才重新注入（2026-11-14 三次生产实证：
    // 重载后渠道页签失效=巡检/认领静默停摆，补回复挂起 30 分钟无人认领）。发布链
    // 收尾自动重开四个桥接渠道页并 keep 常驻——发布即恢复，不再依赖人工补开。
    let pagesRestored = 0;
    const CHANNEL_URLS = [
      'https://weibo.com/chat',
      'https://www.douyin.com/chat',
      'https://message.bilibili.com/',
      'https://www.xiaohongshu.com/chat',
    ];
    for (const u of CHANNEL_URLS) {
      try {
        const cp = await ts.newPage();
        await cp.goto(u, { timeout: 45000 });
        await cp.waitForTimeout(2500);
        pagesRestored++;
      } catch (_) { /* 单渠道失败不阻塞发布链，监控心跳项会兜底报警 */ }
    }
    write({ ok: true, version: ver || null, card, pagesRestored });
    await ts.finish({ keep: 'all' });
  } catch (e) {
    write({ ok: false, step: 'exception', error: String((e && e.message) || e) });
  }
})();
`;
  if (dryRun) return { ok: true, dryRun: true };
  try {
    execFileSync('ego-browser', ['nodejs', '-e', script], { stdio: 'inherit', timeout: 180000 });
  } catch (e) {
    return { ok: false, step: 'ego-browser', error: e.message };
  }
  if (!existsSync(resultPath)) return { ok: false, step: 'no-result' };
  const r = JSON.parse(readFileSync(resultPath, 'utf8'));
  if (!r.ok) return r;
  if (expectVersion && r.version && r.version !== expectVersion) {
    return { ok: false, step: 'version-mismatch', version: r.version };
  }
  return r;
}

/**
 * 在 chrome://extensions 页面尝试「加载未打包的扩展程序」。
 *
 * 实测结论（2026-11-13）：ego-browser 能点到按钮，但 Chrome 对 CDP 合成点击
 * 不弹出文件选择器（waitForFileChooser 8s 超时），macOS 原生目录面板也不受
 * CDP 键盘事件控制。因此本函数只负责"打开页面 + 点按钮 + 尝试接管 chooser"，
 * 失败时调用方必须退回人工指引。
 *
 * @returns {Promise<{ok: boolean, reason?: string, detail?: string}>}
 */
export async function tryLoadUnpacked({ name, loadDir }) {
  const { execFileSync: exec } = await import('node:child_process');
  const resultPath = '/tmp/hivebridge-load-unpacked-result.json';
  if (existsSync(resultPath)) rmSync(resultPath);
  const script = `
const fs = require('node:fs');
const RESULT = ${JSON.stringify(resultPath)};
const DIR = ${JSON.stringify(loadDir)};
const NAME = ${JSON.stringify(name)};
const write = (o) => fs.writeFileSync(RESULT, JSON.stringify(o));
(async () => {
  try {
    const ts = await taskSpace('hivebridge-install');
    const p = await ts.page('p1');
    await p.goto('chrome://extensions/');
    await p.waitForTimeout(1500);
    let snap = await p.snapshot({ scope: 'full_page' });
    let text = typeof snap === 'string' ? snap : JSON.stringify(snap);
    const m = /button \\[ref=(\\d+)\\]\\s*\\n\\s*text "加载未打包的扩展程序"/.exec(text);
    if (!m) { await ts.finish({ keep: [] }); return write({ ok: false, reason: 'load-button-not-found' }); }
    const chooser = p.waitForFileChooser({ timeout: 8000 }).catch((e) => ({ error: String((e && e.message) || e) }));
    await p.click('ref=' + m[1], { label: '加载未打包的扩展程序' });
    const ch = await chooser;
    if (ch && ch.error) { await ts.finish({ keep: [] }); return write({ ok: false, reason: 'chooser-not-opened', detail: ch.error }); }
    const set = await ch.setFiles(DIR).catch((e) => ({ error: String((e && e.message) || e) }));
    await p.waitForTimeout(2000);
    snap = await p.snapshot({ scope: 'full_page' });
    text = typeof snap === 'string' ? snap : JSON.stringify(snap);
    const listed = new RegExp(NAME.split(/[\\s—]/)[0]).test(text);
    write({ ok: !!listed, reason: listed ? null : 'not-listed', setRes: set });
    await ts.finish({ keep: [] });
  } catch (e) {
    write({ ok: false, reason: 'exception', detail: String((e && e.stack) || e) });
  }
})();
`;
  try {
    exec('ego-browser', ['nodejs', '-e', script], { stdio: 'inherit', timeout: 180000 });
  } catch (e) {
    return { ok: false, reason: 'ego-browser', detail: e.message };
  }
  if (!existsSync(resultPath)) return { ok: false, reason: 'no-result' };
  return JSON.parse(readFileSync(resultPath, 'utf8'));
}
