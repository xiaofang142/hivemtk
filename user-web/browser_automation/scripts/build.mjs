// 用 esbuild 把每个入口独立打包为自包含 IIFE（对齐 bridge/scripts/build.mjs）。
// 注意：不 rmSync 整个 dist/（避免把已 Load 的扩展搞坏），只原地覆盖产物。
import { build } from 'esbuild';
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, '..');
const dist = resolve(root, 'dist');

mkdirSync(dist, { recursive: true });

const entries = {
  background: 'src/background/index.js',
  popup: 'src/popup/index.js',
};

const watch = process.argv.includes('--watch');

const commonOpts = {
  bundle: true,
  format: 'iife',
  platform: 'browser',
  target: ['chrome116'],
  minify: !watch,
  sourcemap: watch ? 'inline' : false,
  logLevel: 'info',
};

for (const [name, entry] of Object.entries(entries)) {
  await build({
    ...commonOpts,
    entryPoints: [resolve(root, entry)],
    outfile: resolve(dist, `${name}.js`),
  });
}

// 静态资源：manifest / popup.html / 图标
copyFileSync(resolve(root, 'manifest.json'), resolve(dist, 'manifest.json'));
copyFileSync(resolve(root, 'src/popup/popup.html'), resolve(dist, 'popup.html'));
try {
  mkdirSync(resolve(dist, 'icons'), { recursive: true });
  copyFileSync(resolve(root, 'assets/icons/128.png'), resolve(dist, 'icons/128.png'));
} catch {
  // 图标缺失不阻断构建（Chrome 会用默认图标）
}

// 版本注入（便于 popup 显示构建时间）
const manifest = JSON.parse(readFileSync(resolve(root, 'manifest.json'), 'utf8'));
writeFileSync(
  resolve(dist, 'build-info.json'),
  JSON.stringify({ version: manifest.version, builtAt: new Date().toISOString() }, null, 2),
);

console.log('[browser_automation] 构建完成 → dist/');
