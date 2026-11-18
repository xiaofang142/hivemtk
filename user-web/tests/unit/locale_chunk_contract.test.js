// @vitest-environment node
import { expect, it } from 'vitest';
import { spawnSync } from 'node:child_process';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

it('每个语言独立分块，保留原阈值和全部语言离线策略', () => {
  const url = pathToFileURL(resolve('vite.config.js')).href;
  const script = `import config from ${JSON.stringify(url)};
    const bucket = config.build.rollupOptions.output.manualChunks;
    const codes = ['ar','de','en','es','fr','ja','pt','ru','zh'];
    process.stdout.write(JSON.stringify({chunks: codes.map(code => bucket('/workspace/src/i18n/locales/'+code+'.json')),
      windows: bucket('C:\\\\workspace\\\\src\\\\i18n\\\\locales\\\\en.json'),
      unknown: bucket('/workspace/src/i18n/locales/unknown.json') ?? null,
      vue: bucket('/workspace/node_modules/vue/dist/vue.runtime.esm.js'),
      threshold: config.build.chunkSizeWarningLimit}));`;
  const result = spawnSync(process.execPath, ['--input-type=module', '-e', script], { encoding: 'utf8' });
  expect(result.status, result.stderr).toBe(0);
  const parsed = JSON.parse(result.stdout);
  expect(parsed.chunks).toEqual(['ar', 'de', 'en', 'es', 'fr', 'ja', 'pt', 'ru', 'zh'].map(code => `locale-${code}`));
  expect(parsed.windows).toBe('locale-en');
  expect(parsed.unknown).toBeNull();
  expect(parsed.vue).toBe('vue');
  expect(parsed.threshold).toBe(1600);
});
