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
const { print: _print } = require('./lib/stdout.cjs');


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
// 放宽键名字符类：本仓存在大量以中文字面量直呼的键（如 $t('短链管理')、t('术语表管理')），
// 若限定首字符 [A-Za-z_] 会把它们整类排除在提取之外 —— i18n:check 显示 0 缺失、
// 运行时却吐裸键。故匹配除引号外的任意键名，
// 并保留 \bt 前缀语义（http.get('/api/…')、.split('T') 这类误报靠它排除）。
const T_REGEX = /(?:\$t|\bt\s*)\(\s*['"]([^'"]+)['"]/g;
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

// ---- 主流程 ----
async function main() {
  _print('============================================================');
  _print('  i18n 翻译完整性提取 (OPT-FE-EXT-1)');
  _print('  基准 locale:', FLAGS.base);
  _print('  阈值:', FLAGS.threshold, FLAGS.strict ? '(strict)' : '');
  _print('============================================================\n');

  // 1) 扫描 .vue
  const vueFiles = walk(SRC_DIR);
  _print(`[1/3] 扫描 ${vueFiles.length} 个 .vue 文件 ...`);
  const usedKeys = new Set();
  for (const f of vueFiles) {
    const ks = extractKeysFromVue(f);
    for (const k of ks) {
      // 拼接前缀伪键：t('menu.' + menu.key) 这类动态拼接被正则抽成 'menu.'，
      // 运行时真正取的是 'menu.<key>'，前缀本身永远查不到词条，
      // 当作未使用跳过，免得每个语种恒报 1 条假缺失。
      // 注意保留 '...' 结尾的真键（如 t('创建成功！正在后台自动配置 Bot...')）。
      if (k.endsWith('.') && !k.endsWith('...')) continue;
      usedKeys.add(k);
    }
  }
  _print(`      共提取到 ${usedKeys.size} 个被调用的 key\n`);

  // 2) 加载 locales
  _print('[2/3] 加载 locale 文件 ...');
  const locales = {};
  const resources = JSON.parse(fs.readFileSync(path.join(SRC_DIR, 'i18n/resources.json'), 'utf8'));
  const { assembleResources } = await import('../src/i18n/assembleResources.js');
  const messages = assembleResources(resources, file => {
    try {
      return JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, file), 'utf8'));
    } catch (err) {
      throw new Error(`${file} 读取或解析失败: ${err.message}`, { cause: err });
    }
  });
  for (const [code, message] of Object.entries(messages)) {
    locales[code] = flattenKeys(message);
    _print(`      ${code}: ${locales[code].size} keys`);
  }
  _print();

  if (!locales[FLAGS.base]) {
    console.error(`❌ 基准 locale ${FLAGS.base}.json 不存在或解析失败`);
    process.exit(2);
  }

  // 3) 对比
  _print('[3/3] 对比结果:\n');
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
      _print(`  ✅ ${code}.json: 全部对齐 (${ks.size} keys)`);
    } else {
      _print(`  ⚠️  ${code}.json: 缺 ${missing.length} keys`);
      missing.slice(0, 5).forEach((k) => _print(`       - ${k}`));
      if (missing.length > 5) _print(`       ... 还有 ${missing.length - 5} 个`);
    }
  }

  const totalMissing = Object.values(report.missingInLocale).reduce((s, a) => s + a.length, 0);
  _print();
  _print('------------------------------------------------------------');
  _print(`  基准:           ${FLAGS.base}.json (${locales[FLAGS.base].size} keys)`);
  _print(`  代码中使用的:   ${usedKeys.size} keys`);
  _print(`  总缺失(非基准): ${totalMissing} keys`);
  _print(`  未使用(unused): ${report.unusedInCodeTotal || 0} keys (warn-only)`);
  _print('------------------------------------------------------------');

  // 写出 JSON 报告
  if (FLAGS.json) {
    fs.writeFileSync(FLAGS.json, JSON.stringify(report, null, 2), 'utf8');
    _print(`\n  📄 JSON 报告已写入: ${FLAGS.json}`);
  }

  if (FLAGS.strict && totalMissing > 0) {
    console.error('\n❌ strict 模式下, 缺失 key 即视为错误');
    process.exit(1);
  }
  if (totalMissing > FLAGS.threshold) {
    console.error(`\n❌ 缺失 ${totalMissing} keys > 阈值 ${FLAGS.threshold}`);
    process.exit(1);
  }

  _print('\n✅ i18n 提取检查通过');
  process.exit(0);
}

main().catch(e => {
  console.error('❌ 未捕获错误:', e.message);
  console.error(e.stack);
  process.exit(2);
});
