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
import { existsSync, rmSync, readFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';

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
export function reloadExtension({ name, expectVersion, resultPath, dryRun = false }) {
  if (!name || !/^\d+\.\d+\.\d+$/.test(expectVersion || '')) {
    return { ok: false, step: 'arguments', error: '扩展名称和期望版本必须明确' };
  }
  if (dryRun) return { ok: true, dryRun: true };
  resultPath ||= join(mkdtempSync(join(tmpdir(), 'hivebridge-reload-')), 'result.json');
  if (existsSync(resultPath)) return { ok: false, step: 'result-exists' };
  const spaceName = `hivebridge-reload-${randomUUID()}`;
  const script = String.raw`
const fs = await import('node:fs');
const RESULT = ${JSON.stringify(resultPath)};
const NAME = ${JSON.stringify(name)};
const EXPECTED = ${JSON.stringify(expectVersion)};
const write = (value) => fs.writeFileSync(RESULT, JSON.stringify(value), { flag: 'wx' });
const textOf = (snapshot) => typeof snapshot === 'string' ? snapshot : JSON.stringify(snapshot);
const refs = (text, role, label) => text.split('\n').flatMap(line => {
  if (!line.includes(role + ' ' + JSON.stringify(label))) return [];
  const match = /\[ref=(\d+)[,\]]/.exec(line);
  return match ? [match[1]] : [];
});
const targetCard = (text) => {
  const lines = text.split('\n');
  const names = lines.map((line, index) => ({ line, index })).filter(({ line }) =>
    line.trim().replace(/^- /, '') === 'text ' + JSON.stringify(NAME)
    || line.trim().replace(/^- /, '') === 'heading ' + JSON.stringify(NAME));
  if (names.length !== 1) return null;
  const anchor = names[0].index;
  let depth = lines[anchor].search(/\S/);
  for (let start = anchor - 1; start >= 0; start--) {
    if (!lines[start].trim()) continue;
    const indent = lines[start].search(/\S/);
    if (indent >= depth) continue;
    depth = indent;
    if (!/^(?:- )?(?:article|container)(?: |$)/.test(lines[start].trim())) continue;
    let end = start + 1;
    while (end < lines.length && (!lines[end].trim() || lines[end].search(/\S/) > indent)) end++;
    const card = lines.slice(start, end).join('\n');
    if (refs(card, 'button', '重新加载').length !== 1) continue;
    const versions = [...card.matchAll(/(?:text|StaticText) "(\d+\.\d+\.\d+)"/g)].map(match => match[1]);
    const ids = [...card.matchAll(/text "ID[：:]\s*([a-p]{32})"/g)].map(match => match[1]);
    if (ids.length !== 1) return null;
    return { card, id: ids[0], version: versions.length === 1 ? versions[0] : null };
  }
  return null;
};
(async () => {
  let spaceId;
  try {
    const task = await taskSpace(${JSON.stringify(spaceName)});
    spaceId = task.spaceId;
    const page = task.page('p1');
    await page.goto('chrome://extensions/');
    let text = textOf(await page.snapshot({ scope: 'full_page' }));
    const boxes = refs(text, 'searchbox', '搜索扩展程序');
    if (boxes.length !== 1) return write({ ok: false, step: 'searchbox', spaceId });
    await page.fill('ref=' + boxes[0], NAME);
    await page.waitForFunction((name) => {
      const manager = document.querySelector('extensions-manager');
      const list = manager?.shadowRoot?.querySelector('extensions-item-list');
      const items = [...(list?.shadowRoot?.querySelectorAll('extensions-item') || [])]
        .filter(item => item.getClientRects().length > 0);
      return items.length === 1 && items[0].shadowRoot?.querySelector('#name')?.textContent?.trim() === name;
    }, NAME, { timeout: 15000 });
    text = textOf(await page.snapshot({ scope: 'full_page' }));
    const buttons = refs(text, 'button', '重新加载');
    if (buttons.length !== 1) return write({ ok: false, step: 'reload-button', count: buttons.length, spaceId });
    const before = targetCard(text);
    if (!before || refs(before.card, 'button', '重新加载')[0] !== buttons[0]) {
      return write({ ok: false, step: 'target-card', spaceId });
    }
    await page.click('ref=' + buttons[0], { label: '重新加载目标蜂桥扩展' });
    try {
      await page.waitForSelector('text=' + JSON.stringify(EXPECTED), { state: 'visible', timeout: 15000 });
    } catch { /* 最终快照仍须证明目标卡片的版本。 */ }
    text = textOf(await page.snapshot({ scope: 'full_page' }));
    const after = targetCard(text);
    if (after && after.id !== before.id) {
      return write({ ok: false, step: 'identity-mismatch', spaceId });
    }
    if (!after || after.version !== EXPECTED) {
      return write({ ok: false, step: 'version-mismatch', version: after?.version || null, spaceId });
    }
    write({ ok: true, version: after.version, extensionId: after.id, card: after.card, spaceId });
  } catch (error) {
    write({ ok: false, step: 'exception', error: String(error?.message || error), spaceId });
  }
})();
`;
  try {
    execFileSync('ego-browser', ['nodejs', '-e', script], { stdio: 'inherit', timeout: 180000 });
  } catch (error) {
    return { ok: false, step: 'ego-browser', error: error.message };
  }
  if (!existsSync(resultPath)) return { ok: false, step: 'no-result' };
  let result;
  try { result = JSON.parse(readFileSync(resultPath, 'utf8')); }
  catch { return { ok: false, step: 'invalid-result' }; }
  if (!result.ok) return result;
  if (result.version !== expectVersion || !Number.isInteger(result.spaceId)) {
    return { ok: false, step: 'version-mismatch', version: result.version };
  }
  // 成功证据回传后再执行独立的最后一次清理，不再操作页面。
  try {
    execFileSync('ego-browser', ['nodejs', '-e',
      `const task = await taskSpace(${result.spaceId}); await task.finish({ keep: [] });`],
    { stdio: 'inherit', timeout: 30000 });
  } catch {
    return { ...result, ok: false, step: 'cleanup' };
  }
  return result;
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
