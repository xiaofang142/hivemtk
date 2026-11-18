// @vitest-environment node
import { describe, expect, it } from 'vitest';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, readdirSync, copyFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';

const scripts = resolve('scripts');
const name = '合成HiveBridge发行测试';
const fixedDate = '2026-11-17T06:00:00.000Z';

function fixture() {
  const directory = mkdtempSync(join(tmpdir(), 'hivebridge-publish-contract-'));
  const root = join(directory, 'bridge');
  const load = join(directory, 'loaded');
  const bin = join(directory, 'bin');
  mkdirSync(join(root, 'scripts/lib'), { recursive: true });
  mkdirSync(join(root, 'dist'));
  mkdirSync(join(root, 'release'));
  mkdirSync(load);
  mkdirSync(bin);
  copyFileSync(join(scripts, 'publish.mjs'), join(root, 'scripts/publish.mjs'));
  copyFileSync(join(scripts, 'lib/ego-browser.mjs'), join(root, 'scripts/lib/ego-browser.mjs'));
  writeFileSync(join(root, 'package.json'), JSON.stringify({ version: '1.3.7', type: 'module' }));
  const manifest = { name, version: '1.3.7' };
  writeFileSync(join(root, 'manifest.json'), JSON.stringify(manifest));
  writeFileSync(join(root, 'dist/manifest.json'), JSON.stringify(manifest));
  writeFileSync(join(root, 'dist/popup.js'), 'const server = "https://service.example.test";');
  writeFileSync(join(root, 'release/hivebridge-1.3.7.zip'), '合成归档，解包命令由桩校验');
  writeFileSync(join(load, 'manifest.json'), JSON.stringify({ name, version: '1.3.6' }));
  writeFileSync(join(load, 'old-evidence.txt'), '必须保留的旧版本证据');
  writeFileSync(join(bin, 'unzip'), `#!${process.execPath}\nprocess.stdout.write(${JSON.stringify(JSON.stringify(manifest))});\n`, { mode: 0o755 });
  const synced = join(directory, 'rsync-receipt.txt');
  writeFileSync(join(bin, 'rsync'), `#!${process.execPath}
    const fs = require('node:fs');
    const args = process.argv.slice(2);
    fs.writeFileSync(${JSON.stringify(synced)}, JSON.stringify(args));
    fs.copyFileSync(args[args.length-2] + 'manifest.json', args[args.length-1] + 'manifest.json');
  `, { mode: 0o755 });
  const wrapper = join(root, 'run.mjs');
  writeFileSync(wrapper, `const OriginalDate = Date;
    globalThis.Date = class extends OriginalDate {
      constructor(...args) { super(...(args.length ? args : [${JSON.stringify(fixedDate)}])); }
      static now() { return OriginalDate.parse(${JSON.stringify(fixedDate)}); }
    };
    await import(${JSON.stringify(pathToFileURL(join(root, 'scripts/publish.mjs')).href)});
  `);
  const run = (...args) => spawnSync(process.execPath, [wrapper, '--skip-build', ...args], {
    cwd: root, encoding: 'utf8', env: { ...process.env, PATH: `${bin}:${process.env.PATH}`,
      HIVEBRIDGE_LOAD_DIR: load, BRIDGE_ONLINE_SERVER: 'https://service.example.test' },
  });
  return { directory, root, load, synced, run };
}

describe('发行备份只增不删', () => {
  it('安装不会删除或修改第六份以外的旧回滚副本', () => {
    const f = fixture();
    const backups = Array.from({ length: 7 }, (_, index) => `hivebridge-rollback-v1.0.${index}-old`);
    for (const backup of backups) {
      mkdirSync(join(f.directory, backup));
      writeFileSync(join(f.directory, backup, 'evidence.txt'), backup);
    }
    const result = f.run();
    expect(result.status, result.stderr).toBe(0);
    for (const backup of backups) {
      expect(existsSync(join(f.directory, backup, 'evidence.txt')), backup).toBe(true);
      expect(readFileSync(join(f.directory, backup, 'evidence.txt'), 'utf8')).toBe(backup);
    }
    const created = readdirSync(f.directory).filter(entry => entry.startsWith('hivebridge-rollback-') && !backups.includes(entry));
    expect(created).toHaveLength(1);
    expect(readFileSync(join(f.directory, created[0], 'old-evidence.txt'), 'utf8')).toBe('必须保留的旧版本证据');
    expect(JSON.parse(readFileSync(join(f.load, 'manifest.json'), 'utf8')).version).toBe('1.3.7');
  });

  it('同秒备份路径已存在时，不合并旧证据、不开始安装', () => {
    const f = fixture();
    const backup = join(f.directory, 'hivebridge-rollback-v1.3.6-2026-11-17_06-00-00');
    mkdirSync(backup);
    writeFileSync(join(backup, 'old-evidence.txt'), '前次独立备份，禁止覆盖');
    const result = f.run();
    expect(result.status).not.toBe(0);
    expect(readFileSync(join(backup, 'old-evidence.txt'), 'utf8')).toBe('前次独立备份，禁止覆盖');
    expect(existsSync(f.synced)).toBe(false);
    expect(JSON.parse(readFileSync(join(f.load, 'manifest.json'), 'utf8')).version).toBe('1.3.6');
  });

  it('错误产物在备份前拒绝，dry-run不创建备份或同步', () => {
    const f = fixture();
    const dry = f.run('--dry-run');
    expect(dry.status, dry.stderr).toBe(0);
    expect(readdirSync(f.directory).filter(entry => entry.startsWith('hivebridge-rollback-'))).toEqual([]);
    expect(existsSync(f.synced)).toBe(false);
    writeFileSync(join(f.root, 'dist/manifest.json'), JSON.stringify({ name, version: '1.3.5' }));
    const invalid = f.run();
    expect(invalid.status).not.toBe(0);
    expect(existsSync(f.synced)).toBe(false);
    expect(readdirSync(f.directory).filter(entry => entry.startsWith('hivebridge-rollback-'))).toEqual([]);
  });
});
