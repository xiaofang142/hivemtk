// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { mkdtempSync, readFileSync, existsSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const calls = vi.hoisted(() => ({ scripts: [], execute: null }));
vi.mock('node:child_process', () => ({ execFileSync: vi.fn((_cmd, args) => {
  if (args[0] === 'nodejs') {
    calls.scripts.push(args.at(-1));
    calls.execute?.(args.at(-1));
  }
}) }));
import { reloadExtension } from '../scripts/lib/ego-browser.mjs';

const text = version => `- searchbox "搜索扩展程序" [ref=1]\n- article [ref=2]\n  - heading "HiveBridge 蜂桥"\n  - text "${version}"\n  - text "ID：ijjfoaghckkdimglbbgdhhagkhemhnam"\n  - button "重新加载" [ref=3]`;
const extensionName = 'HiveBridge 蜂桥 — 抖音/小红书/TikTok/闲鱼/快手/京东/B站/微博 私信桥接';
const treeSnapshot = (version, extra = '') => `root
  container
    banner
      searchbox "搜索扩展程序" [ref=1]
    main
      container
        text "已与${extensionName}关联"
        container
          container
            image
            container
              container
                heading
                  text ${JSON.stringify(extensionName)}
                text ${JSON.stringify(version)}
              text "合成扩展描述"
              text "ID：ijjfoaghckkdimglbbgdhhagkhemhnam"
          container
            button [ref=8]
              text "详情"
            button [ref=9]
              text "移除"
            button "重新加载" [ref=3]
            switch "开启，扩展程序已启用" [ref=10]
${extra}
  text "1 个相符的搜索结果"`;

beforeEach(() => { calls.scripts = []; calls.execute = null; });

async function runBrowser({ version = '1.3.7', ambiguous = false, targetName = 'HiveBridge 蜂桥', cleanupFails = false,
  tree = false, filteringPending = false, extra = '', changedId = false } = {}) {
  const directory = mkdtempSync(join(tmpdir(), 'bridge-reload-test-'));
  const resultPath = join(directory, 'result.json');
  const receiptsPath = join(directory, 'receipts.json');
  const initial = tree ? treeSnapshot('1.3.6', extra) : '9.9.9\n' + text('1.3.6');
  const suffix = ambiguous ? '\n- button "重新加载" [ref=4]' : '';
  const snapshot = value => tree ? treeSnapshot(value, extra)
    : text(value).replace('heading "HiveBridge 蜂桥"', `heading ${JSON.stringify(targetName)}`);
  const { execFileSync } = await vi.importActual('node:child_process');
  calls.execute = source => {
    // 原生Node执行原ESM脚本；仅TaskSpace/Page为边界替身，不替换import或文件写入。
    const harness = `
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
const receiptsPath = ${JSON.stringify(receiptsPath)};
const pageCalls = existsSync(receiptsPath) ? JSON.parse(readFileSync(receiptsPath, 'utf8')) : [];
let filtered = false;
let filterReady = ${JSON.stringify(!filteringPending)};
let reloaded = false;
const page = {
  goto: async url => { pageCalls.push(['goto', url]); },
  snapshot: async () => !filtered ? ${JSON.stringify(initial)}
    : (reloaded ? ${JSON.stringify((snapshot(version) + suffix).replace(
      'ijjfoaghckkdimglbbgdhhagkhemhnam', changedId ? 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' : 'ijjfoaghckkdimglbbgdhhagkhemhnam'))}
      : ${JSON.stringify(snapshot('1.3.6') + suffix)}),
  fill: async (selector, value) => {
    if (selector !== 'ref=1') throw new Error('selector must identify the snapshot element');
    pageCalls.push(['fill', selector, value]); filtered = true;
  },
  click: async selector => {
    if (!filterReady) throw new Error('filter has not settled');
    if (selector !== 'ref=3') throw new Error('selector must identify the snapshot element');
    pageCalls.push(['click', selector]); reloaded = true;
  },
  waitForTimeout: async () => {},
  waitForFunction: async (fn, arg) => {
    const item = { getClientRects: () => [{}], shadowRoot: { querySelector: () => ({ textContent: ${JSON.stringify(tree ? extensionName : 'HiveBridge 蜂桥')} }) } };
    globalThis.document = { querySelector: () => ({ shadowRoot: { querySelector: () => ({ shadowRoot: {
      querySelectorAll: () => [item],
    } }) } }) };
    if (!fn(arg)) throw new Error('filter predicate did not confirm one matching extension');
    pageCalls.push(['filter-ready']); filterReady = true; return true;
  },
  waitForSelector: async () => true,
};
const fakeTask = {
  spaceId: 77, page: () => page,
  newPage: async () => { throw new Error('automatic channel page creation is forbidden'); },
  finish: async options => {
    const evidence = JSON.parse(readFileSync(${JSON.stringify(resultPath)}, 'utf8'));
    if (!evidence.ok) throw new Error('cannot finish before verified success');
    if (${JSON.stringify(cleanupFails)}) throw new Error('合成清理失败');
    pageCalls.push(['finish', options]);
  },
};
globalThis.taskSpace = async () => fakeTask;
${source.replace('(async () => {', 'await (async () => {')}
writeFileSync(receiptsPath, JSON.stringify(pageCalls));
`;
    execFileSync(process.execPath, ['--input-type=module', '-e', harness], { timeout: 10000 });
  };
  const returned = reloadExtension({ name: tree ? extensionName : 'HiveBridge 蜂桥', expectVersion: '1.3.7', resultPath });
  const source = calls.scripts[0];
  const result = existsSync(resultPath) ? JSON.parse(readFileSync(resultPath, 'utf8')) : null;
  const pageCalls = existsSync(receiptsPath) ? JSON.parse(readFileSync(receiptsPath, 'utf8')) : [];
  return { result, returned, pageCalls, source };
}

describe('扩展重载使用当前浏览器接口', () => {
  it('真实树形快照只取目标卡片，不混入旁边的版本文字', async () => {
    const extra = '      container\n        text "其它扩展"\n        text "9.9.9"';
    const { result, returned } = await runBrowser({ tree: true, extra });
    expect(returned).toMatchObject({ ok: true, version: '1.3.7' });
    expect(result.card).toContain('ijjfoaghckkdimglbbgdhhagkhemhnam');
    expect(result.card).not.toContain('9.9.9');
  });

  it('目标名称已出现时仍须等待异步搜索过滤完成', async () => {
    const { result, pageCalls } = await runBrowser({ tree: true, filteringPending: true });
    expect(result).toMatchObject({ ok: true, version: '1.3.7' });
    expect(pageCalls.findIndex(c => c[0] === 'filter-ready')).toBeLessThan(pageCalls.findIndex(c => c[0] === 'click'));
  });

  it('相同名称的卡片ID变化不能当作原扩展重载成功', async () => {
    const { result } = await runBrowser({ tree: true, changedId: true });
    expect(result).toMatchObject({ ok: false, step: 'identity-mismatch' });
    expect(calls.scripts).toHaveLength(1);
  });

  it('精确填写和点击唯一卡片，严格读取目标版本，不自动打开渠道页', async () => {
    const { result, returned, pageCalls, source } = await runBrowser();
    expect(result).toMatchObject({ ok: true, version: '1.3.7' });
    expect(returned).toMatchObject({ ok: true, version: '1.3.7' });
    expect(pageCalls.filter(c => c[0] === 'goto')).toEqual([['goto', 'chrome://extensions/']]);
    expect(pageCalls.filter(c => c[0] === 'click')).toEqual([['click', 'ref=3']]);
    expect(source).not.toMatch(/\brequire\s*\(/);
    expect(source).not.toContain('newPage(');
    expect(source).not.toContain("keep: 'all'");
    expect(calls.scripts).toHaveLength(2);
    expect(pageCalls.at(-1)).toEqual(['finish', { keep: [] }]);
  });

  it('多个重载按钮时不点击', async () => {
    const { result, pageCalls } = await runBrowser({ ambiguous: true });
    expect(result).toMatchObject({ ok: false, step: 'reload-button' });
    expect(pageCalls.some(c => c[0] === 'click')).toBe(false);
    expect(calls.scripts).toHaveLength(1);
  });

  it.each(['', '1.3.6'])('目标版本%s缺失或不一致时不得宣称成功', async version => {
    const { result } = await runBrowser({ version });
    expect(result).toMatchObject({ ok: false, step: 'version-mismatch' });
    expect(calls.scripts).toHaveLength(1);
  });

  it('唯一按钮属于其它扩展时也不点击', async () => {
    const { result, pageCalls } = await runBrowser({ targetName: 'Other extension' });
    expect(result).toMatchObject({ ok: false, step: 'target-card' });
    expect(pageCalls.some(c => c[0] === 'click')).toBe(false);
    expect(calls.scripts).toHaveLength(1);
  });

  it('清理失败不得把整个交付流程标记成功，并保留重载证据', async () => {
    const { result, returned } = await runBrowser({ cleanupFails: true });
    expect(result).toMatchObject({ ok: true, version: '1.3.7' });
    expect(returned).toMatchObject({ ok: false, step: 'cleanup' });
    expect(calls.scripts).toHaveLength(2);
  });

  it('已有结果文件不会被删除或覆盖', () => {
    const resultPath = join(mkdtempSync(join(tmpdir(), 'bridge-existing-result-')), 'result.json');
    const preserved = '{"evidence":"原有结果"}';
    writeFileSync(resultPath, preserved);
    expect(reloadExtension({ name: 'HiveBridge 蜂桥', expectVersion: '1.3.7', resultPath }))
      .toMatchObject({ ok: false, step: 'result-exists' });
    expect(readFileSync(resultPath, 'utf8')).toBe(preserved);
    expect(calls.scripts).toHaveLength(0);
  });

  it.each([
    { name: '', expectVersion: '1.3.7' },
    { name: 'HiveBridge 蜂桥', expectVersion: '' },
    { name: 'HiveBridge 蜂桥', expectVersion: 'not-a-version' },
  ])('名称或版本缺失不启动浏览器：%j', options => {
    expect(reloadExtension(options)).toMatchObject({ ok: false, step: 'arguments' });
    expect(calls.scripts).toHaveLength(0);
  });
});
