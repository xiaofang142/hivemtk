#!/usr/bin/env node
// HiveBridge 安装链执行器（本机，无 CI 依赖）
//
// 覆盖「租户侧」的完整动作：拿到 zip -> 解压 -> 装进本机 Chrome 加载目录 ->
// 重新加载。发布链（publish.mjs）是「构建 -> 安装」；本脚本是「下载/取包 ->
// 安装」，两者共用 scripts/lib/ego-browser.mjs 的重载能力。
//
// 用法：
//   node scripts/install-extension.mjs --zip release/hivebridge-1.3.0.zip
//   node scripts/install-extension.mjs --dir /path/to/unpacked
//   node scripts/install-extension.mjs --download          # 从用户端页面下载（需平台已上架）
//   node scripts/install-extension.mjs --zip <zip> --dry-run
//
// 三种取包来源（互斥，必选其一）：
//   --zip <path>   本地 zip（npm run release 产物，或从用户端页面下载回来的）
//   --dir <path>   已解压目录（根目录直接是 manifest.json）
//   --download     ego-browser 打开用户端「桥接扩展安装包」页，点下载按钮，
//                  download.saveAs() 存成 zip 再走后续流程。
//                  前提：平台运营已把 browser_extension 资产上架——当前线上
//                  该页显示「当前部署未接入平台，或平台尚未上架桥接扩展包」，
//                  没有下载按钮，脚本会明确报这个错并退出（不是脚本坏）。
//
// 安装策略（关键）：
//   - 目标加载目录 = HIVEBRIDGE_LOAD_DIR > ~/.hivebridge-publish.json.loadDir
//   - 该目录**已被 Chrome 登记**（路径不变 => 扩展 ID 不变 => chrome.storage 不丢）
//     => 直接 rsync 覆盖 + ego-browser 重新加载，全自动，无任何人工步骤。
//   - 该目录尚不存在（全新机器）=> 解压就位后调「加载未打包的扩展程序」。
//     这一步**无法全自动**：Chrome 对 CDP 合成点击不弹文件选择器
//     （实测 waitForFileChooser 8s 超时），macOS 原生目录面板也不受 CDP
//     键盘控制。脚本会点开按钮并尝试接管 chooser，失败则打印一次性人工
//     三步指引（点按钮 / 选目录 / 点打开），之后所有版本更新都回到全自动。

import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync, mkdirSync, rmSync, cpSync } from 'node:fs';
import { homedir } from 'node:os';
import { resolve, dirname, basename } from 'node:path';
import { fileURLToPath } from 'node:url';
import { reloadExtension, tryLoadUnpacked } from './lib/ego-browser.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const opt = {
  zip: argValue('--zip'),
  dir: argValue('--dir'),
  download: args.includes('--download'),
  noReload: args.includes('--no-reload'),
  dryRun: args.includes('--dry-run'),
};

function argValue(flag) {
  const i = args.indexOf(flag);
  return i >= 0 && args[i + 1] && !args[i + 1].startsWith('--') ? args[i + 1] : null;
}

const log = (s) => console.log(s);
const fail = (s) => {
  console.error(`\n❌ ${s}\n`);
  process.exit(1);
};

const manifest = JSON.parse(readFileSync(resolve(root, 'manifest.json'), 'utf8'));
const extName = manifest.name;

// ---------------------------------------------------------------------------
// 加载目录解析（与 publish.mjs 同源，两边必须一致）
// ---------------------------------------------------------------------------
function resolveLoadDir() {
  const env = process.env.HIVEBRIDGE_LOAD_DIR;
  if (env) return env;
  const cfgPath = resolve(homedir(), '.hivebridge-publish.json');
  if (existsSync(cfgPath)) {
    try {
      const cfg = JSON.parse(readFileSync(cfgPath, 'utf8'));
      if (cfg && cfg.loadDir) return cfg.loadDir;
    } catch {
      /* 配置坏了就当没有，走下面的报错 */
    }
  }
  fail(
    '未配置 Chrome 加载目录。设置环境变量 HIVEBRIDGE_LOAD_DIR，或在 ' +
      `${cfgPath} 写 { "loadDir": "/绝对/路径" }。\n` +
      '   查看方法：chrome://extensions -> HiveBridge 卡片「详情」-> 加载来源路径',
  );
}

const loadDir = resolveLoadDir();
const alreadyLoaded = existsSync(resolve(loadDir, 'manifest.json'));
log(`==> 目标加载目录：${loadDir}`);
log(`==> 该目录${alreadyLoaded ? '已被 Chrome 登记（走覆盖更新，全自动）' : '尚不存在（首次安装，需一次人工点选目录）'}`);

// ---------------------------------------------------------------------------
// 1) 取包
// ---------------------------------------------------------------------------
let source = null; // { kind: 'zip'|'dir', path }
const sources = [opt.zip && `--zip ${opt.zip}`, opt.dir && `--dir ${opt.dir}`, opt.download && '--download'].filter(Boolean);
if (sources.length === 0) fail('必须指定取包来源之一：--zip <path> / --dir <path> / --download');
if (sources.length > 1) fail(`取包来源只能选一个，收到了 ${sources.length} 个：${sources.join('、')}`);

const tmpUnpack = resolve('/tmp', `hivebridge-unpack-${Date.now()}`);

if (opt.download) {
  log(`\n==> [1/4] 从用户端页面下载安装包（ego-browser）`);
  const resultPath = '/tmp/hivebridge-download-result.json';
  if (existsSync(resultPath)) rmSync(resultPath);
  const script = `
const fs = require('node:fs');
const RESULT = ${JSON.stringify(resultPath)};
const write = (o) => fs.writeFileSync(RESULT, JSON.stringify(o));
(async () => {
  try {
    const ts = await taskSpace('hivebridge-install');
    const p = await ts.page('p1');
    await p.goto('https://work.xapptool.cn/#/bridge/extension');
    await p.waitForTimeout(3500);
    const snap = await p.snapshot({ scope: 'full_page' });
    const text = typeof snap === 'string' ? snap : JSON.stringify(snap);
    if (/平台尚未上架桥接扩展包|未接入平台/.test(text)) {
      await ts.finish({ keep: [] });
      return write({ ok: false, reason: 'platform-no-asset', text: text.slice(0, 400) });
    }
    const m = /(?:button|link) \\[ref=(\\d+)[,\\]]\\s*\\n\\s*text "([^"]*(?:下载|安装包)[^"]*)"/.exec(text);
    if (!m) { await ts.finish({ keep: [] }); return write({ ok: false, reason: 'download-button-not-found' }); }
    const dl = p.waitForEvent('download', { timeout: 60000 }).catch((e) => ({ error: String((e && e.message) || e) }));
    await p.click('ref=' + m[1], { label: m[2] });
    const d = await dl;
    if (d && d.error) { await ts.finish({ keep: [] }); return write({ ok: false, reason: 'download-timeout', detail: d.error }); }
    const out = '/tmp/hivebridge-downloaded.zip';
    await d.saveAs(out);
    write({ ok: true, path: out, filename: d.suggestedFilename(), url: d.url() });
    await ts.finish({ keep: [] });
  } catch (e) {
    write({ ok: false, reason: 'exception', detail: String((e && e.stack) || e) });
  }
})();
`;
  try {
    execFileSync('ego-browser', ['nodejs', '-e', script], { stdio: 'inherit', timeout: 180000 });
  } catch (e) {
    fail(`ego-browser 调用失败：${e.message}`);
  }
  if (!existsSync(resultPath)) fail('ego-browser 未回写结果，下载状态未知');
  const r = JSON.parse(readFileSync(resultPath, 'utf8'));
  if (!r.ok) {
    if (r.reason === 'platform-no-asset') {
      fail(
        '用户端页面显示「当前部署未接入平台，或平台尚未上架桥接扩展包」——租户下载源里没有包。\n' +
          '   这一步只能平台运营在平台管理后台上传 release/hivebridge-<版本>.zip 为\n' +
          '   browser_extension 资产（平台端无上传 API，见 docs/operations/RELEASE-PIPELINE.md 链 C）。\n' +
          '   上传成功后重跑本命令即可自动下载安装。',
      );
    }
    fail(`下载失败（reason=${r.reason}${r.detail ? ` detail=${r.detail}` : ''}）`);
  }
  source = { kind: 'zip', path: r.path };
  log(`    ✅ 已下载 ${r.filename} -> ${r.path}`);
} else if (opt.zip) {
  const zp = resolve(opt.zip);
  if (!existsSync(zp)) fail(`zip 不存在：${zp}`);
  source = { kind: 'zip', path: zp };
  log(`\n==> [1/4] 使用本地 zip：${zp}`);
} else {
  const dp = resolve(opt.dir);
  if (!existsSync(resolve(dp, 'manifest.json'))) fail(`目录里没有 manifest.json：${dp}`);
  source = { kind: 'dir', path: dp };
  log(`\n==> [1/4] 使用已解压目录：${dp}`);
}

// ---------------------------------------------------------------------------
// 2) 解压（zip 才需要）
// ---------------------------------------------------------------------------
let unpacked = source.path;
if (source.kind === 'zip') {
  log(`\n==> [2/4] 解压到 ${tmpUnpack}`);
  if (opt.dryRun) {
    log('    （dry-run）跳过解压');
  } else {
    rmSync(tmpUnpack, { recursive: true, force: true });
    mkdirSync(tmpUnpack, { recursive: true });
    try {
      execFileSync('unzip', ['-q', source.path, '-d', tmpUnpack], { stdio: 'inherit' });
    } catch (e) {
      fail(`解压失败：${e.message}`);
    }
    const inner = resolve(tmpUnpack, 'manifest.json');
    if (existsSync(inner)) {
      unpacked = tmpUnpack;
    } else {
      // 有些 zip 会多包一层目录，往里找一层
      const { readdirSync } = await import('node:fs');
      const sub = readdirSync(tmpUnpack).filter((f) => existsSync(resolve(tmpUnpack, f, 'manifest.json')));
      if (sub.length !== 1) fail('解压后找不到 manifest.json（根目录或唯一一层子目录内应直接是 manifest.json）');
      unpacked = resolve(tmpUnpack, sub[0]);
    }
    const m = JSON.parse(readFileSync(resolve(unpacked, 'manifest.json'), 'utf8'));
    if (m.name !== extName) fail(`manifest.name 不一致：包内 "${m.name}" vs 仓库 "${extName}"——装错包了`);
    log(`    ✅ 包内版本 v${m.version}，name 校验通过`);
  }
} else {
  log(`\n==> [2/4] 跳过解压（已是目录）`);
}

// ---------------------------------------------------------------------------
// 3) 装进加载目录
// ---------------------------------------------------------------------------
log(`\n==> [3/4] 安装到 ${loadDir}`);
if (opt.dryRun) {
  log('    （dry-run）跳过文件安装');
} else if (alreadyLoaded) {
  // 已登记：保留路径，覆盖内容。先备份，和 publish.mjs 同口径。
  const stamp = new Date().toISOString().replace(/[:.]/g, '-').replace('T', '_').slice(0, 19);
  const oldVer = JSON.parse(readFileSync(resolve(loadDir, 'manifest.json'), 'utf8')).version;
  const backupDir = resolve(dirname(loadDir), `hivebridge-rollback-v${oldVer}-${stamp}`);
  cpSync(loadDir, backupDir, { recursive: true });
  log(`    ✅ 旧版备份到 ${basename(backupDir)}/`);
  try {
    execFileSync('rsync', ['-a', '--delete', `${unpacked}/`, `${loadDir}/`], { stdio: 'inherit' });
  } catch (e) {
    fail(`rsync 失败：${e.message}`);
  }
  const nowVer = JSON.parse(readFileSync(resolve(loadDir, 'manifest.json'), 'utf8')).version;
  log(`    ✅ 已覆盖安装，目录内版本 v${nowVer}`);
} else {
  mkdirSync(dirname(loadDir), { recursive: true });
  cpSync(unpacked, loadDir, { recursive: true });
  const m = JSON.parse(readFileSync(resolve(loadDir, 'manifest.json'), 'utf8'));
  log(`    ✅ 已解压就位（v${m.version}）——目录路径此后不要改，改了扩展 ID 会变、chrome.storage 会丢`);
  log(`\n==> [3.5/4] 首次加载：调「加载未打包的扩展程序」`);
  const r = await tryLoadUnpacked({ name: extName, loadDir });
  if (r.ok) {
    log('    ✅ ego-browser 已接管文件选择器并完成加载');
  } else {
    log(`    ⚠️  自动加载未完成（reason=${r.reason}${r.detail ? ` detail=${r.detail}` : ''}）`);
    log('    原因：Chrome 对 CDP 合成点击不弹出文件选择器，macOS 原生目录面板也不受');
    log('    CDP 键盘控制。请人工完成这一次（之后所有版本更新都全自动）：');
    log(`      1. chrome://extensions 右上角开启「开发者模式」`);
    log(`      2. 点「加载未打包的扩展程序」`);
    log(`      3. 选择目录：${loadDir}  -> 点「打开」`);
  }
}

// ---------------------------------------------------------------------------
// 4) 重新加载
// ---------------------------------------------------------------------------
const wantVer = opt.dryRun
  ? manifest.version
  : alreadyLoaded
    ? JSON.parse(readFileSync(resolve(loadDir, 'manifest.json'), 'utf8')).version
    : manifest.version;
if (opt.noReload) {
  log(`\n==> [4/4] 跳过重新加载（--no-reload）。手动收尾：chrome://extensions 搜 HiveBridge -> 点「重新加载」`);
} else {
  log(`\n==> [4/4] 重新加载扩展（ego-browser）`);
  const r = reloadExtension({ name: extName, expectVersion: wantVer, dryRun: opt.dryRun });
  if (opt.dryRun) {
    log('    （dry-run）跳过重载');
  } else if (!r.ok) {
    log(`    ⚠️  重载未完成（step=${r.step}${r.error ? ` error=${r.error}` : ''}）`);
    log('    请手动：chrome://extensions 搜 HiveBridge -> 点「重新加载」');
  } else {
    log(`    ✅ 已重新加载，快照中版本=${r.version || '未读到'}`);
  }
}

if (existsSync(tmpUnpack)) rmSync(tmpUnpack, { recursive: true, force: true });
log(`\n✅ 安装链完成。\n`);
