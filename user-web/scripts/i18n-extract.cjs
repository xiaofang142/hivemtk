#!/usr/bin/env node
// =============================================================================
// i18n-extract.cjs — vue-i18n 翻译完整性提取与对照工具
// 任务编号: OPT-FE-EXT-1
// 创建日期: 2026-08-16
//
// 功能:
//   1) 扫描 src 下的 .vue 文件中所有 $t('key') / $t("key") / t('key') 调用, 提取 key 集合
//   2) 读取 src/i18n/locales 下的 .json 文件, 解析为扁平 key 集合
//   3) 对比调用与翻译文件:
//      - 列出源代码中使用但翻译文件中缺失的 key (missing in locale)
//      - 列出翻译文件中存在但代码中未使用的 key (unused in code)  [warn-only]
//      - 列出代码中使用的 key 在所有 locale 中的对齐情况
//   4) 以退出码表达结果:
//      - 0: 通过 (缺少数 <= THRESHOLD)
//      - 1: 缺少数 > 阈值
//      - 2: 解析错误
//
// 用法:
//   node scripts/i18n-extract.cjs                # 扫描 + 对比 (zh.json 作为基准)
//   node scripts/i18n-extract.cjs --base en     # 用 en.json 作为基准
//   node scripts/i18n-extract.cjs --strict      # 任何缺失即视为错误
//   node scripts/i18n-extract.cjs --json out.json  # 输出 JSON 报告
//
// 集成: 在 package.json 增加 "prebuild": "node scripts/i18n-extract.cjs"
// =============================================================================

'use strict';

const fs = require('fs');
const path = require('path');

const ROOT = path.resolve(__dirname, '..');
const SRC_DIR = path.join(ROOT, 'src');
const LOCALES_DIR = path.join(SRC_DIR, 'i18n', 'locales');

// ---- CLI 参数 ----
const args = process.argv.slice(2);
const FLAGS = {
  base: 'zh',
  strict: false,
  json: null,
  threshold: 50,  // 默认 50, 兼容新语种 placeholder (es/fr/de/ru/pt) 尚未翻译
};
for (let i = 0; i < args.length; i++) {
  if (args[i] === '--base' && args[i + 1]) { FLAGS.base = args[++i]; }
  else if (args[i] === '--strict') { FLAGS.strict = true; }
  else if (args[i] === '--threshold' && args[i + 1]) { FLAGS.threshold = parseInt(args[++i], 10); }
  else if (args[i] === '--json' && args[i + 1]) { FLAGS.json = args[++i]; }
}

// ---- 工具函数 ----
function walk(dir, out = []) {
  if (!fs.existsSync(dir)) return out;
  for (const ent of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, ent.name);
    if (ent.isDirectory()) {
      if (ent.name === 'node_modules' || ent.name === 'dist' || ent.name === '.git') continue;
      walk(p, out);
    } else if (ent.isFile() && p.endsWith('.vue')) {
      out.push(p);
    }
  }
  return out;
}

// 提取 $t('xxx') / $t("xxx") / t('xxx') / t("xxx")
// 支持带参数: $t('key', { ... })  $t('key', n, { ... })
// key 限制: 字母/数字/点/下划线/中划线 (vue-i18n 合法字符), 首字符必须为字母/下划线
const T_REGEX = /(?:\$t|\bt\s*)\(\s*['"]([A-Za-z_][A-Za-z0-9_.-]*)['"]/g;
function extractKeysFromVue(file) {
  const txt = fs.readFileSync(file, 'utf8');
  const keys = new Set();
  let m;
  while ((m = T_REGEX.exec(txt)) !== null) {
    if (m[1] && !m[1].includes('\n')) keys.add(m[1]);
  }
  return keys;
}

// 递归展平 JSON 对象 key
function flattenKeys(obj, prefix = '', out = new Set()) {
  if (obj == null) return out;
  if (typeof obj !== 'object') {
    out.add(prefix);
    return out;
  }
  for (const [k, v] of Object.entries(obj)) {
    const full = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      flattenKeys(v, full, out);
    } else {
      out.add(full);
    }
  }
  return out;
}

function loadLocaleKeys(file) {
  if (!fs.existsSync(file)) return null;
  const raw = fs.readFileSync(file, 'utf8');
  try {
    return flattenKeys(JSON.parse(raw));
  } catch (e) {
    return { error: e.message };
  }
}

// ---- 主流程 ----
function main() {
  console.log('============================================================');
  console.log('  i18n 翻译完整性提取 (OPT-FE-EXT-1)');
  console.log('  基准 locale:', FLAGS.base);
  console.log('  阈值:', FLAGS.threshold, FLAGS.strict ? '(strict)' : '');
  console.log('============================================================\n');

  // 1) 扫描 .vue
  const vueFiles = walk(SRC_DIR);
  console.log(`[1/3] 扫描 ${vueFiles.length} 个 .vue 文件 ...`);
  const usedKeys = new Set();
  for (const f of vueFiles) {
    const ks = extractKeysFromVue(f);
    for (const k of ks) usedKeys.add(k);
  }
  console.log(`      共提取到 ${usedKeys.size} 个被调用的 key\n`);

  // 2) 加载 locales
  console.log('[2/3] 加载 locale 文件 ...');
  const locales = {};
  const localeFiles = fs.existsSync(LOCALES_DIR)
    ? fs.readdirSync(LOCALES_DIR).filter((f) => f.endsWith('.json'))
    : [];
  for (const f of localeFiles) {
    const code = path.basename(f, '.json');
    const ks = loadLocaleKeys(path.join(LOCALES_DIR, f));
    if (ks && !ks.error) {
      locales[code] = ks;
      console.log(`      ${code}.json: ${ks.size} keys`);
    } else if (ks && ks.error) {
      console.log(`      ${code}.json: ❌ 解析失败: ${ks.error}`);
    }
  }
  console.log();

  if (!locales[FLAGS.base]) {
    console.error(`❌ 基准 locale ${FLAGS.base}.json 不存在或解析失败`);
    process.exit(2);
  }

  // 3) 对比
  console.log('[3/3] 对比结果:\n');
  const report = {
    base: FLAGS.base,
    usedCount: usedKeys.size,
    locales: {},
    missingInLocale: {},
    unusedInCode: {},
  };

  for (const [code, ks] of Object.entries(locales)) {
    report.locales[code] = ks.size;
    if (code === FLAGS.base) continue;

    const missing = [...usedKeys].filter((k) => !ks.has(k)).sort();
    const unused = [...ks].filter((k) => !usedKeys.has(k)).sort();
    report.missingInLocale[code] = missing;
    report.unusedInCode[code] = unused.slice(0, 20);  // 仅报告前 20 个 unused
    report.unusedInCodeTotal = (report.unusedInCodeTotal || 0) + unused.length;

    if (missing.length === 0) {
      console.log(`  ✅ ${code}.json: 全部对齐 (${ks.size} keys)`);
    } else {
      console.log(`  ⚠️  ${code}.json: 缺 ${missing.length} keys`);
      missing.slice(0, 5).forEach((k) => console.log(`       - ${k}`));
      if (missing.length > 5) console.log(`       ... 还有 ${missing.length - 5} 个`);
    }
  }

  const totalMissing = Object.values(report.missingInLocale).reduce((s, a) => s + a.length, 0);
  console.log();
  console.log('------------------------------------------------------------');
  console.log(`  基准:           ${FLAGS.base}.json (${locales[FLAGS.base].size} keys)`);
  console.log(`  代码中使用的:   ${usedKeys.size} keys`);
  console.log(`  总缺失(非基准): ${totalMissing} keys`);
  console.log(`  未使用(unused): ${report.unusedInCodeTotal || 0} keys (warn-only)`);
  console.log('------------------------------------------------------------');

  // 写出 JSON 报告
  if (FLAGS.json) {
    fs.writeFileSync(FLAGS.json, JSON.stringify(report, null, 2), 'utf8');
    console.log(`\n  📄 JSON 报告已写入: ${FLAGS.json}`);
  }

  if (FLAGS.strict && totalMissing > 0) {
    console.error('\n❌ strict 模式下, 缺失 key 即视为错误');
    process.exit(1);
  }
  if (totalMissing > FLAGS.threshold) {
    console.error(`\n❌ 缺失 ${totalMissing} keys > 阈值 ${FLAGS.threshold}`);
    process.exit(1);
  }

  console.log('\n✅ i18n 提取检查通过');
  process.exit(0);
}

try {
  main();
} catch (e) {
  console.error('❌ 未捕获错误:', e.message);
  console.error(e.stack);
  process.exit(2);
}
