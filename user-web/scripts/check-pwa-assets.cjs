#!/usr/bin/env node
/**
 * check-pwa-assets.cjs —— PWA / 浏览器扩展图标资源校验
 *
 * 背景（2026-09-16 修复的两个真实缺陷，二者都不会让构建或 lint 失败）：
 *   1. public/manifest.webmanifest 声明 /logo.png 为 192x192，实际却是
 *      1328x1328 / 953 KB —— 而 index.html 把它当 PNG favicon 在首屏加载，
 *      等于每个用户首屏多下 953 KB，直接拖慢 LCP。
 *   2. browser_automation/assets/icons/128.png 是 1x1 占位图（70 字节），
 *      manifest 却声明 128 —— 扩展管理页 / 应用商店显示为空白。
 *   补此门禁，避免同类问题再次静默回归。
 *
 * 校验项：
 *   - manifest 声明的 sizes 必须与 PNG 实际像素一致
 *   - 图标文件必须存在，且不是 1x1 占位图
 *   - 体积分档：≤256px（首屏可见）上限 200 KB；≥512px（仅安装时取用）上限 400 KB
 *
 * 路径说明：
 *   - PWA manifest 的 src 相对 public/
 *   - 扩展 manifest 的 src 相对 **dist/**（构建产物根）；
 *     scripts/build.mjs 负责 assets/icons/* -> dist/icons/* 的拷贝，
 *     故源码侧的 assets/icons/*.png 单独走 EXTRA_PNGS 检查。
 *
 * 用法：node scripts/check-pwa-assets.cjs [--json]
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const MAX_SMALL = Number(process.env.PWA_ICON_MAX_KB || 200) * 1024; // ≤256px
const MAX_LARGE = Number(process.env.PWA_ICON_MAX_LARGE_KB || 400) * 1024; // ≥512px

const TARGETS = [
  { manifest: 'public/manifest.webmanifest', base: 'public' },
  { manifest: 'browser_automation/manifest.json', base: 'browser_automation/dist' },
];

// 源码侧图标（构建前就应合规，避免把占位图带进产物）
const EXTRA_PNGS = ['browser_automation/assets/icons/128.png'];

/** 读 PNG IHDR 取宽高（纯字节解析，无第三方依赖，跨平台） */
function pngSize(file) {
  const fd = fs.openSync(file, 'r');
  try {
    const buf = Buffer.alloc(24);
    fs.readSync(fd, buf, 0, 24, 0);
    if (buf.readUInt32BE(0) !== 0x89504e47) return null;
    return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
  } finally {
    fs.closeSync(fd);
  }
}

const problems = [];
const checked = [];

function checkPng(abs, label, declared) {
  if (!/\.png$/i.test(abs)) return; // SVG 等矢量图标不校验像素
  if (!fs.existsSync(abs)) {
    problems.push({ level: 'error', file: label, msg: '图标文件不存在' });
    return;
  }
  const stat = fs.statSync(abs);
  const dim = pngSize(abs);
  if (!dim) {
    problems.push({ level: 'error', file: label, msg: '不是合法 PNG，无法校验尺寸' });
    return;
  }
  checked.push({ file: label, declared, actual: `${dim.width}x${dim.height}`, bytes: stat.size });

  const m = /^(\d+)x(\d+)$/.exec(declared || '');
  if (m && (Number(m[1]) !== dim.width || Number(m[2]) !== dim.height)) {
    problems.push({ level: 'error', file: label, msg: `声明 ${declared}，实际 ${dim.width}x${dim.height}` });
  }
  if (dim.width <= 1 || dim.height <= 1) {
    problems.push({ level: 'error', file: label, msg: '疑似 1x1 占位图' });
    return;
  }
  const budget = dim.width >= 512 ? MAX_LARGE : MAX_SMALL;
  if (stat.size > budget) {
    problems.push({
      level: 'error',
      file: label,
      msg: `体积 ${(stat.size / 1024).toFixed(0)} KB 超过 ${dim.width >= 512 ? '≥512px' : '≤256px'} 档上限 ${(budget / 1024).toFixed(0)} KB`,
    });
  }
}

for (const t of TARGETS) {
  const manifestPath = path.join(ROOT, t.manifest);
  if (!fs.existsSync(manifestPath)) {
    problems.push({ level: 'warn', file: t.manifest, msg: 'manifest 不存在，跳过' });
    continue;
  }
  let manifest;
  try {
    manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
  } catch (e) {
    problems.push({ level: 'error', file: t.manifest, msg: `JSON 解析失败: ${e.message}` });
    continue;
  }

  let icons = manifest.icons;
  // Chrome 扩展 manifest 的 icons 是 { "128": "icons/128.png" } 映射，
  // PWA manifest 是数组 —— 两种都支持。
  if (icons && !Array.isArray(icons)) {
    icons = Object.entries(icons).map(([size, src]) => ({ src, sizes: `${size}x${size}` }));
  }
  if (!icons || icons.length === 0) continue;

  for (const icon of icons) {
    const rel = String(icon.src || '').replace(/^\//, '');
    const abs = path.join(ROOT, t.base, rel);
    // dist 是构建产物：未构建时降级为警告，交由 EXTRA_PNGS 从源码侧兜底
    if (/dist[\\/]/.test(t.base) && !fs.existsSync(abs)) {
      problems.push({ level: 'warn', file: `${t.manifest} -> ${icon.src}`, msg: 'dist 未构建，跳过（源码侧图标另行校验）' });
      continue;
    }
    checkPng(abs, `${t.manifest} -> ${icon.src}`, icon.sizes || 'any');
  }
}

for (const rel of EXTRA_PNGS) {
  checkPng(path.join(ROOT, rel), rel, null);
}

if (process.argv.includes('--json')) {
  console.log(JSON.stringify({ checked, problems }, null, 2));
} else {
  console.log('=== PWA / 扩展图标资源校验 ===\n');
  for (const c of checked) {
    console.log(`  ✅ ${c.file}  声明=${c.declared || '-'} 实际=${c.actual} ${(c.bytes / 1024).toFixed(1)} KB`);
  }
  console.log('');
  if (problems.length === 0) {
    console.log(`✅ 全部通过（${checked.length} 个图标）`);
  } else {
    for (const p of problems) console.log(`  ${p.level === 'error' ? '❌' : '⚠️ '} ${p.file}: ${p.msg}`);
  }
}

process.exit(problems.some((p) => p.level === 'error') ? 1 : 0);
