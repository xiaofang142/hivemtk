// 用 esbuild 把每个入口独立打包为自包含 IIFE（MV3 content/background 多入口场景最稳）。
import { build } from 'esbuild';
import { copyFileSync, mkdirSync, readFileSync, writeFileSync, rmSync, cpSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, '..');
const dist = resolve(root, 'dist');

// 注意：不要 rmSync 整个 dist/ 再重建。
// 旧版 build 会 rmSync(dist, {recursive:true}) 把目录整体删空再写入；若此刻用户 Chrome 仍
// Load 着该 dist/，开发者模式下文件瞬间全空会让 Chrome 把扩展标记为「已损坏/失效」，
// 表现为「点击扩展图标不弹出 popup 面板」。改为只确保目录存在、原地覆盖产物，避免
// 构建过程中 dist/ 出现「整体消失」窗口，从而不会把已加载的扩展搞坏。
mkdirSync(dist, { recursive: true });

const entries = {
  background: 'src/background/index.js',
  'content-douyin': 'src/content/douyin.js',
  'content-xhs': 'src/content/xhs.js',
  'content-tiktok': 'src/content/tiktok.js',
  'content-xianyu': 'src/content/xianyu.js',
  'content-kuaishou': 'src/content/kuaishou.js',
  'content-jd': 'src/content/jd.js',
  'content-bilibili': 'src/content/bilibili.js',
  'content-weibo': 'src/content/weibo.js',
  'content-pdd': 'src/content/pdd.js',
  popup: 'src/popup/index.js',
};

const watch = process.argv.includes('--watch');

for (const [name, entry] of Object.entries(entries)) {
  const opts = {
    entryPoints: [resolve(root, entry)],
    bundle: true,
    format: 'iife',
    outfile: resolve(dist, `${name}.js`),
    platform: 'browser',
    target: 'es2020',
    minify: !watch,
    sourcemap: watch,
    logLevel: 'info',
    // 线上（SaaS）地址只在发行构建时注入；未设时源码常量得到空串，
    // 自部署用户的弹窗默认仍指向本机（见 src/core/constants.js 隐私说明）。
    define: {
      // key 用专用标识符而非 process.env.*：MV3 没有 process 全局，替换
      // process.env.X 只会留下 `typeof process<"u" && …` 的运行时短路，
      // 注入被静默丢弃（2026-11-04 二次审核实测：带 env 构建出的包仍不预填）。
      __BRIDGE_ONLINE_SERVER__: JSON.stringify(process.env.BRIDGE_ONLINE_SERVER || ''),
    },
  };
  if (watch) {
    const ctx = await build.context(opts);
    await ctx.watch();
  } else {
    await build(opts);
  }
}

// popup.html：复制并把脚本指向打包后的 popup.js（classic script，符合 MV3）
let html = readFileSync(resolve(root, 'src/popup/index.html'), 'utf8');
html = html.replace('./index.js', './popup.js');
writeFileSync(resolve(dist, 'popup.html'), html);
copyFileSync(resolve(root, 'manifest.json'), resolve(dist, 'manifest.json'));

// 图标：manifest 引用 assets/icons 下的 PNG，复制到 dist/icons（原地覆盖，避免破坏已 Load 的扩展）
try {
  cpSync(resolve(root, 'assets/icons'), resolve(dist, 'icons'), { recursive: true });
} catch (e) {
  console.warn('复制图标失败（可忽略）', e && e.message);
}

console.log('build done ->', dist);
