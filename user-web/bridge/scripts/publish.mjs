#!/usr/bin/env node
// HiveBridge 发布链执行器（本机，无 CI 依赖）
//
// 一条命令走完「构建 -> 校验 -> 装进本机 Chrome 加载目录 ->（可选）重新加载扩展」：
//
//   BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn \
//   HIVEBRIDGE_LOAD_DIR=/Users/xiaofang/Documents/tool/hivebridge-1.0.2 \
//   npm run publish -- --reload
//
// 选项：
//   --reload       装完后调 ego-browser 打开 chrome://extensions 并点「重新加载」
//                  （明确两步单元素操作：填搜索框过滤到唯一卡片 -> 点唯一重载按钮；
//                   不是枚举式批量点击，符合全局规则 -1）
//   --skip-build   跳过构建，只把现有 dist 安装/重载（调试用）
//   --self-hosted  明确构建**自部署**产物（弹窗默认 localhost:8204）。
//                  默认不带这个标志时必须提供 BRIDGE_ONLINE_SERVER——
//                  否则容易把 localhost 包当线上包发出去，租户下载后连不上云端。
//   --dry-run      只打印将执行的动作，不动任何文件
//
// 加载目录来源（优先级从高到低）：
//   1) 环境变量 HIVEBRIDGE_LOAD_DIR
//   2) ~/.hivebridge-publish.json 的 { "loadDir": "..." }（个人配置，勿提交仓库）
// 为什么必须显式配置：Chrome 里「加载已解压的扩展程序」指向的目录因人而异，
// 且**路径必须保持不变**才能保住扩展 ID 与 chrome.storage（已发送缓存、
// user-server 地址配置）——移除重装会丢 storage，可能导致历史消息重复发送。
//
// 回滚：每次安装前把旧目录整份复制到 <loadDir>/../hivebridge-rollback-<版本>-<时间戳>/
// （默认保留最近 5 份），需要回滚时 rsync 回去再点一次重新加载即可。

import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync, cpSync, rmSync, readdirSync, statSync } from 'node:fs';
import { homedir } from 'node:os';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { reloadExtension } from './lib/ego-browser.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const opt = {
  reload: args.includes('--reload'),
  skipBuild: args.includes('--skip-build'),
  selfHosted: args.includes('--self-hosted'),
  dryRun: args.includes('--dry-run'),
};

const log = (s) => console.log(s);
const fail = (s) => {
  console.error(`\n❌ ${s}\n`);
  process.exit(1);
};

const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
const version = pkg.version;
const dist = resolve(root, 'dist');
const releaseDir = resolve(root, 'release');

// ---------------------------------------------------------------------------
// 0) 版本一致性预检（与 scripts/release.mjs 同口径，提前失败）
//    Chrome 读 manifest.json 的版本，zip 名读 package.json；两者不一致时
//    Chrome 里看到的版本和用户下载到的包名会对不上。
// ---------------------------------------------------------------------------
const manifest = JSON.parse(readFileSync(resolve(root, 'manifest.json'), 'utf8'));
if (manifest.version !== version) {
  fail(`版本不一致：package.json=${version} / manifest.json=${manifest.version}\n   请先改成同一个版本再发版。`);
}

// 0b) 发行口径门：线上包必须注入云端地址。
//     历史教训（1.0.4）：不带 BRIDGE_ONLINE_SERVER 构建出来的包弹窗默认
//     localhost:8204，租户下载后连不上云端，等于发了个不可用的包。
const onlineServer = process.env.BRIDGE_ONLINE_SERVER || '';
if (!opt.skipBuild && !opt.selfHosted && !onlineServer) {
  fail(
    '未提供 BRIDGE_ONLINE_SERVER。\n' +
    '   官方发行包必须注入线上地址，例如：\n' +
    '     BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn npm run publish -- --reload\n' +
    '   如果确实要发自部署产物（默认 localhost:8204），请显式加 --self-hosted。',
  );
}

log(`\n🐝 HiveBridge 发布链 v${version}`);
log(`   构建口径：${opt.skipBuild ? '跳过构建（--skip-build）' : onlineServer ? `线上地址注入 ${onlineServer}` : '自部署（localhost:8204）'}`);
if (opt.dryRun) log('   模式：--dry-run（只打印，不动文件）');

// ---------------------------------------------------------------------------
// 1) 加载目录解析 + 安全检查
//    rsync --delete 会删掉目标目录里所有多余文件，指错目录就是灾难，
//    所以装之前必须校验：目录存在、里面是 manifest.json、且 name 对得上。
// ---------------------------------------------------------------------------
let loadDir = process.env.HIVEBRIDGE_LOAD_DIR || '';
if (!loadDir) {
  const cfgPath = resolve(homedir(), '.hivebridge-publish.json');
  if (existsSync(cfgPath)) {
    try {
      const cfg = JSON.parse(readFileSync(cfgPath, 'utf8'));
      if (cfg && typeof cfg.loadDir === 'string' && cfg.loadDir) loadDir = cfg.loadDir;
    } catch (e) {
      fail(`读取 ${cfgPath} 失败：${e.message}`);
    }
  }
}
if (!loadDir) {
  fail(
    '未配置 Chrome 扩展加载目录。请用环境变量或配置文件二选一：\n' +
    '   export HIVEBRIDGE_LOAD_DIR=/路径/到/Chrome加载的扩展目录\n' +
    '   echo \'{"loadDir":"/路径/到/Chrome加载的扩展目录"}\' > ~/.hivebridge-publish.json',
  );
}
loadDir = resolve(loadDir.replace(/^~/, homedir()));

if (!existsSync(loadDir)) fail(`加载目录不存在：${loadDir}`);
const installedManifestPath = resolve(loadDir, 'manifest.json');
if (!existsSync(installedManifestPath)) {
  fail(`${loadDir} 下没有 manifest.json，不像一个已加载的扩展目录——拒绝 --delete 同步。`);
}
let installedVersion = null;
try {
  const im = JSON.parse(readFileSync(installedManifestPath, 'utf8'));
  installedVersion = im.version;
  if (im.name !== manifest.name) {
    fail(`目录名不匹配：${loadDir} 的 manifest.name=${im.name}，仓库是 ${manifest.name}——指错目录了。`);
  }
} catch (e) {
  fail(`读取 ${installedManifestPath} 失败：${e.message}`);
}
log(`\n==> 加载目录：${loadDir}`);
log(`    当前已装版本 v${installedVersion} -> 将安装 v${version}`);

// ---------------------------------------------------------------------------
// 2) 构建 + 打包（复用 npm run release，它自带「版本一致 + CHANGELOGS 有条目」前置门）
// ---------------------------------------------------------------------------
if (!opt.skipBuild) {
  log(`\n==> [1/4] 构建与打包（npm run release）`);
  if (!opt.dryRun) {
    execFileSync('npm', ['run', 'release'], { cwd: root, stdio: 'inherit' });
  }
} else {
  log(`\n==> [1/4] 跳过构建`);
}
if (!existsSync(dist)) fail(`dist 不存在：${dist}（先跑一次构建）`);

// ---------------------------------------------------------------------------
// 3) 产物校验：版本三处一致 + 注入串在位
// ---------------------------------------------------------------------------
log(`\n==> [2/4] 产物校验`);
const checks = [];
const distManifest = JSON.parse(readFileSync(resolve(dist, 'manifest.json'), 'utf8'));
checks.push([`dist/manifest.json 版本 = ${version}`, distManifest.version === version]);

const zipPath = resolve(releaseDir, `hivebridge-${version}.zip`);
if (existsSync(zipPath)) {
  let zipManifest = '';
  try {
    zipManifest = execFileSync('unzip', ['-p', zipPath, 'manifest.json'], { encoding: 'utf8' });
  } catch {
    checks.push(['zip 可读取', false]);
  }
  if (zipManifest) {
    checks.push([`zip 内 manifest.json 版本 = ${version}`, JSON.parse(zipManifest).version === version]);
  }
} else {
  checks.push([`release/hivebridge-${version}.zip 存在`, false]);
}

if (onlineServer) {
  const popup = readFileSync(resolve(dist, 'popup.js'), 'utf8');
  checks.push([`popup.js 含注入地址 ${onlineServer}`, popup.includes(onlineServer)]);
}

const failed = checks.filter(([, ok]) => !ok);
for (const [label, ok] of checks) log(`    ${ok ? '✅' : '❌'} ${label}`);
if (failed.length) fail(`产物校验未通过（${failed.length} 项），终止安装。`);

// ---------------------------------------------------------------------------
// 4) 安装：备份旧目录 -> rsync --delete -> 复核版本
//    备份保留最近 5 份，更老的自动清掉（回滚副本是排障用的，不是归档）。
// ---------------------------------------------------------------------------
log(`\n==> [3/4] 安装到 Chrome 加载目录`);
const stamp = new Date().toISOString().replace(/[:.]/g, '-').replace('T', '_').slice(0, 19);
const backupDir = resolve(dirname(loadDir), `hivebridge-rollback-v${installedVersion}-${stamp}`);
if (opt.dryRun) {
  log(`    （dry-run）将备份 ${loadDir} -> ${backupDir}`);
  log(`    （dry-run）将 rsync -a --delete ${dist}/ -> ${loadDir}/`);
} else {
  cpSync(loadDir, backupDir, { recursive: true });
  log(`    已备份旧版 -> ${backupDir}`);
  execFileSync('rsync', ['-a', '--delete', `${dist}/`, `${loadDir}/`], { stdio: 'inherit' });
  const nowVersion = JSON.parse(readFileSync(installedManifestPath, 'utf8')).version;
  if (nowVersion !== version) {
    fail(`安装后校验失败：${loadDir} 里是 v${nowVersion}，期望 v${version}。可从 ${backupDir} 回滚。`);
  }
  log(`    已同步，目录内版本 v${nowVersion}`);

  // 清理过老的回滚副本（保留最近 5 份）
  const siblings = readdirSync(dirname(loadDir))
    .filter((n) => /^hivebridge-rollback-v/.test(n))
    .map((n) => ({ n, t: statSync(resolve(dirname(loadDir), n)).mtimeMs }))
    .sort((a, b) => b.t - a.t);
  for (const old of siblings.slice(5)) {
    rmSync(resolve(dirname(loadDir), old.n), { recursive: true, force: true });
    log(`    清理过老回滚副本 ${old.n}`);
  }
}

// ---------------------------------------------------------------------------
// 5) 重新加载扩展（ego-browser，明确两步单元素操作）
//    输出不回传 stdout（ego-browser 把子进程输出直接透传到终端），
//    所以用结果文件回收状态。
// ---------------------------------------------------------------------------
if (!opt.reload) {
  log(`\n==> [4/4] 跳过重新加载（未传 --reload）`);
  log(`\n✅ 完成。手动收尾：chrome://extensions 搜 HiveBridge -> 点「重新加载」\n`);
} else {
  log(`\n==> [4/4] 重新加载扩展（ego-browser）`);
  const r = reloadExtension({ name: manifest.name, expectVersion: version, dryRun: opt.dryRun });
  if (!opt.dryRun) {
    if (!r.ok) {
      fail(
        `重新加载未完成（step=${r.step}${r.count ? ` count=${r.count}` : ''}${r.error ? ` error=${r.error}` : ''}）。\n` +
          '   请手动：chrome://extensions 搜 HiveBridge -> 点「重新加载」',
      );
    }
    log(`    ✅ 已重新加载，快照中版本=${r.version || '未读到'}`);
  } else {
    log('    （dry-run）将调 ego-browser 打开 chrome://extensions 并点「重新加载」');
  }
  log(`\n✅ 发布链完成：v${version} 已构建、已安装、已重新加载。\n`);
}
