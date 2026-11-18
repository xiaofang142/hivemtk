// @vitest-environment node
import { describe, expect, it } from 'vitest';
import { spawnSync, execFileSync } from 'node:child_process';
import { readFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import * as sass from 'sass';

const root = process.cwd();
const styles = resolve(root, 'src/styles');
const configuration = readFileSync(resolve(root, 'vite.config.js'), 'utf8');
const compile = (source) => {
  const warnings = [];
  const result = sass.compileString(source, {
    url: pathToFileURL(resolve(styles, 'contract.scss')),
    loadPaths: [styles],
    logger: { warn: text => warnings.push(text) },
  });
  return { css: result.css, warnings };
};

describe('原生ESM配置与Sass样式契约', () => {
  it('Node无需CommonJS注入即可加载配置，并保留Git版本语义', () => {
    const outputDirectory = mkdtempSync(resolve(tmpdir(), 'build-config-contract-'));
    const script = `import config from ${JSON.stringify(pathToFileURL(resolve(root, 'vite.config.js')).href)};
      import { readFileSync } from 'node:fs';
      process.chdir(${JSON.stringify(outputDirectory)});
      config.plugins.find(p => p?.name === 'generate-version-json').closeBundle();
      process.stdout.write('CONTRACT:' + JSON.stringify({ alias: config.resolve.alias['@'], buildId: JSON.parse(readFileSync('dist/version.json', 'utf8')).buildId }));`;
    // 完整插件不要求在此执行构建；仅原生模块加载和别名即能验证ESM边界。
    const loaded = spawnSync(process.execPath, ['--input-type=module', '-e', script], { cwd: root, encoding: 'utf8' });
    expect(loaded.status, loaded.stderr).toBe(0);
    const result = JSON.parse(loaded.stdout.split('CONTRACT:')[1]);
    expect(result.alias).toBe(resolve(root, 'src'));
    const gitHash = execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
    expect(result.buildId).toMatch(new RegExp(`^${gitHash}-\\d+$`));
    expect(configuration).not.toMatch(/\brequire\s*\(/);
    expect(configuration).not.toMatch(/\b__dirname\b/);
  });

  it('全局变量用模块导入且所有变量生成的CSS不变', () => {
    const injected = configuration.match(/additionalData:\s*(['"])(.*?)\1/s)?.[2];
    expect(injected).toBeTruthy();
    const variables = readFileSync(resolve(styles, 'variables.scss'), 'utf8');
    const names = [...variables.matchAll(/^\$([\w-]+):/gm)].map(match => match[1]);
    const probe = `.contract { ${names.map(name => `--${name}: #{$${name}};`).join('\n')} }`;
    const old = compile(`@import "./variables.scss";\n${probe}`);
    const current = compile(`${injected.replace('@/styles/', './')}\n${probe}`);
    expect(current.css).toBe(old.css);
    expect(current.warnings).toEqual([]);
  });

  it('入口reset模块化保留完整样式顺序与CSS字节', () => {
    const source = readFileSync(resolve(styles, 'index.scss'), 'utf8');
    const injected = configuration.match(/additionalData:\s*(['"])(.*?)\1/s)?.[2];
    expect(injected).toBeTruthy();
    const oldSource = source.replace(/@use\s+(["']\.\/reset\.scss["']);/, '@import $1;');
    const old = compile(`@import "./variables.scss";\n${oldSource}`);
    const current = compile(`${injected.replace('@/styles/', './')}\n${source}`);
    expect(current.css).toBe(old.css);
    expect(current.warnings).toEqual([]);
  });
});
