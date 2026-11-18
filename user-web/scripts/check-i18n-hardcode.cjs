#!/usr/bin/env node
const { print: _print } = require('./lib/stdout.cjs');
/**
 * 视图硬编码中文统计棘轮（OPT-FE-06，2026-10-24）
 *
 * 规划口径（未完成事项实施规划 §4.1 FE-06）：platform-web 29 个 .vue 的 `$t(` 命中为 0
 * ——视图从未国际化。这不是"补覆盖率脚本"而是"分批替换"；本脚本统计 src/views 下的
 * 模板与脚本里的硬编码中文行数，挂基线**只许降**——每替换一批页面就下调 baseline。
 *
 * 口径：含 CJK 字符的**行**数（一行多条文案只计一行，避免标点抖动放大读数）；
 * 排除注释行（// /*、<!--）与 console/log。user-web 起步基线在
 * scripts/i18n-hardcode-baseline.json，platform-web 迁移时同法建自己的。
 *
 * 用法：node scripts/check-i18n-hardcode.cjs [--json out.json]
 * 非零退出 = 超出基线（新硬编码中文入视图）。
 */
const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const VIEWS = path.join(ROOT, 'src', 'views');
const BASELINE = path.join(__dirname, 'i18n-hardcode-baseline.json');
const CJK = /[\u4e00-\u9fff]/;

function listVue(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) listVue(p, out);
    else if (e.name.endsWith('.vue')) out.push(p);
  }
  return out;
}

function countHardcoded(file) {
  const lines = fs.readFileSync(file, 'utf8').split('\n');
  let n = 0;
  let inBlockComment = false;
  for (const raw of lines) {
    const line = raw.trim();
    if (inBlockComment) {
      if (line.includes('*/')) inBlockComment = false;
      continue;
    }
    if (line.startsWith('/*')) { inBlockComment = true; continue; }
    if (line.startsWith('//') || line.startsWith('<!--')) continue;
    if (/console\.(log|warn|error|info)/.test(line)) continue;
    if (CJK.test(line)) n++;
  }
  return n;
}

const args = process.argv.slice(2);
const jsonOut = args.includes('--json') ? args[args.indexOf('--json') + 1] : null;

const files = listVue(VIEWS);
const perFile = {};
let total = 0;
for (const f of files) {
  const n = countHardcoded(f);
  if (n > 0) {
    perFile[path.relative(ROOT, f)] = n;
    total += n;
  }
}

let baseline = { total: Number.MAX_SAFE_INTEGER, note: '' };
if (fs.existsSync(BASELINE)) baseline = JSON.parse(fs.readFileSync(BASELINE, 'utf8'));

const report = { total, files: Object.keys(perFile).length, per_file: perFile, baseline_total: baseline.total };
if (jsonOut) fs.writeFileSync(jsonOut, JSON.stringify(report, null, 2));

_print(`[i18n-hardcode] 视图硬编码中文：${total} 行 / ${report.files} 文件（基线 ${baseline.total}）`);
if (total > baseline.total) {
  console.error(`[i18n-hardcode] ❌ 超出基线（${total} > ${baseline.total}）：新增硬编码中文须走 $t()；确需上调基线请改 scripts/i18n-hardcode-baseline.json（显式棘轮动作）`);
  process.exit(1);
}
if (total < baseline.total) {
  console.error(`[i18n-hardcode] ⚠️ 基线收窄（${total} < ${baseline.total}）：已替换的页面请把 baseline 下调到 ${total}——棘轮不许高于现实`);
  process.exit(1);
}
_print('[i18n-hardcode] ✅ 与基线一致');
