// @vitest-environment node
// release-browser-api.test.js reloadExtension（scripts/lib/ego-browser.mjs）单元契约。
//
// 2026-11-19 随实现重写（快照文本解析 → shadow-DOM 逐元素定位）按新契约重写：
// 旧实现（搜索框/快照 ref/缩进回溯）在 Chrome 新快照结构上三个脆弱点全踩，
// 新实现改为 extensions-item(#name 唯一匹配)→#dev-reload-button 直点。
// 本测试用假 shadow-DOM 在 Node 里直接执行脚本生成的 evaluate 函数，锁死：
//   ① 唯一命中才点击 + 回读版本一致才算成功；
//   ② 同名多 item 歧义守卫（不点错卡片）；
//   ③ #dev-reload-button 缺失不冒充成功；
//   ④ 目标缺失/重载后 id 变化不宣称成功；
//   ⑤ 版本未变（重载没生效）不得宣称成功。
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { mkdtempSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const calls = vi.hoisted(() => ({ scripts: [] }));
vi.mock('node:child_process', () => ({ execFileSync: vi.fn((_cmd, args) => {
  if (args[0] === 'nodejs') calls.scripts.push(args.at(-1));
}) }));
import { reloadExtension } from '../scripts/lib/ego-browser.mjs';

const NAME = 'HiveBridge 蜂桥 — 抖音/小红书/TikTok/闲鱼/快手/京东/B站/微博 私信桥接';
const ID = 'ijjfoaghckkdimglbbgdhhagkhemhnam';

// makeFakeShadowDOM 构造 extensions 页 shadow 树的最小仿真。
// items: [{ name, version, id, withButton }]；afterState 可在重载后改变（版本/id）。
function makeFakeShadowDOM(items, { afterItems = null } = {}) {
  let current = items;
  const itemNode = (def) => {
    const shadow = {
      _children: {},
      querySelector(sel) { return shadow._children[sel] || null; },
      querySelectorAll(sel) { return shadow._children[sel] ? [shadow._children[sel]] : []; },
    };
    shadow._children['#name'] = { textContent: def.name };
    shadow._children['#extension-version'] = { textContent: def.version };
    shadow._children['#version'] = { textContent: def.version };
    shadow._children['#dev-reload-button'] = def.withButton === false ? null : {
      click() { if (afterItems) current = afterItems; def.clicked = (def.clicked || 0) + 1; },
    };
    return {
      id: def.id,
      getClientRects: () => [{}],
      shadowRoot: shadow,
    };
  };
  const list = { shadowRoot: { querySelectorAll: () => current.map(itemNode) } };
  const manager = { shadowRoot: { querySelector: (sel) => sel === 'extensions-item-list' ? list : null } };
  return {
    document: { querySelector: (sel) => sel === 'extensions-manager' ? manager : null },
    versionNow: () => (current[0] || {}).version,
    idNow: () => (current[0] || {}).id,
  };
}

beforeEach(() => { calls.scripts = []; });

async function runBrowser({ items = [{ name: NAME, version: '1.3.8', id: ID }], afterItems = null, expectVersion = '1.3.9' } = {}) {
  const directory = mkdtempSync(join(tmpdir(), 'bridge-reload-test-'));
  const resultPath = join(directory, 'result.json');
  const dom = makeFakeShadowDOM(items, { afterItems });
  const { execFileSync } = await vi.importActual('node:child_process');
  const saved = { doc: globalThis.document };
  calls.execute = async () => {
    // 直接执行 ego 脚本体：taskSpace/page 打桩，document 用假 shadow-DOM。
    const src = calls.scripts[0] || '';
    const task = {
      spaceId: 77,
      page: () => ({
        goto: async () => {},
        waitForFunction: async (_fn, arg) => { globalThis.document = dom.document; },
        evaluate: async (fn, arg) => { globalThis.document = dom.document; return fn(arg); },
      }),
      finish: async () => {},
    };
    globalThis.taskSpace = async () => task;
    const url = 'data:text/javascript;base64,' + Buffer.from(src, 'utf8').toString('base64');
    await import(url);
  };
  try {
    const result = reloadExtension({ name: NAME, expectVersion, resultPath });
    // 同步等待 ego 子进程桩执行完成
    await calls.execute();
    const read = () => { try { return JSON.parse(readResult(resultPath)); } catch { return null; } };
    return { result, read };
  } finally {
    globalThis.document = saved.doc;
  }
}

function readResult(p) { return existsSync(p) ? (require('node:fs')).readFileSync(p, 'utf8') : ''; }

describe('reloadExtension（shadow-DOM 实现）', () => {
  it('① 唯一命中：点击 #dev-reload-button，版本回读一致 → ok=true', async () => {
    const { result, read } = await runBrowser({
      items: [{ name: NAME, version: '1.3.8', id: ID }],
      afterItems: [{ name: NAME, version: '1.3.9', id: ID }],
      expectVersion: '1.3.9',
    });
    expect(read()).toBeTruthy();
    const r = read();
    expect(r.ok).toBe(true);
    expect(r.version).toBe('1.3.9');
    expect(r.extensionId).toBe(ID);
  });

  it('② 同名多 item：歧义守卫生效，不点击不宣称成功', async () => {
    const { read } = await runBrowser({
      items: [
        { name: NAME, version: '1.3.8', id: ID },
        { name: NAME, version: '1.3.8', id: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' },
      ],
    });
    const r = read();
    expect(r?.ok).toBe(false);
    expect(r?.reason).toBe('ambiguous-name');
  });

  it('③ #dev-reload-button 缺失：不冒充成功', async () => {
    const { read } = await runBrowser({
      items: [{ name: NAME, version: '1.3.8', id: ID, withButton: false }],
    });
    const r = read();
    expect(r?.ok).toBe(false);
    expect(r?.reason).toBe('reload-button-missing');
  });

  it('④ 目标缺失：item-not-found', async () => {
    const { read } = await runBrowser({
      items: [{ name: '另一个扩展', version: '1.0.0', id: ID }],
    });
    const r = read();
    expect(r?.ok).toBe(false);
    expect(r?.reason).toBe('item-not-found');
  });

  it('⑤ 重载后 id 变化：identity-mismatch 不宣称成功', async () => {
    const { read } = await runBrowser({
      items: [{ name: NAME, version: '1.3.8', id: ID }],
      afterItems: [{ name: NAME, version: '1.3.9', id: 'cccccccccccccccccccccccccccccccc' }],
      expectVersion: '1.3.9',
    });
    const r = read();
    expect(r?.ok).toBe(false);
    expect(r?.step).toBe('identity-mismatch');
  });

  it('⑥ 版本未变（重载未生效）：version-mismatch 不宣称成功', async () => {
    const { read } = await runBrowser({
      items: [{ name: NAME, version: '1.3.8', id: ID }],
      afterItems: [{ name: NAME, version: '1.3.8', id: ID }],
      expectVersion: '1.3.9',
    });
    const r = read();
    expect(r?.ok).toBe(false);
    expect(r?.step).toBe('version-mismatch');
  });
});
