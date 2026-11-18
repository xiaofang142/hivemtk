import { describe, expect, it, vi } from 'vitest';
import { Console } from 'node:console';
import { PassThrough } from 'node:stream';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);

describe('Node正常输出契约', () => {
  it('保留标准Console格式、参数顺序、对象检查和换行，不改为stderr', () => {
    const { print } = require('../../scripts/lib/stdout.cjs');
    const stdout = new PassThrough();
    const stderr = new PassThrough();
    const reference = new Console({ stdout, stderr });
    let expected = '';
    stdout.on('data', chunk => { expected += chunk.toString(); });
    const calls = [[], ['普通输出'], ['%s:%d %%', '结果', 42], [{ a: 1, b: ['x'] }], ['换行\n保留', undefined, null]];
    for (const args of calls) reference.log(...args);
    const write = vi.spyOn(process.stdout, 'write').mockImplementation(() => true);
    const errorWrite = vi.spyOn(process.stderr, 'write').mockImplementation(() => true);
    try {
      for (const args of calls) print(...args);
      expect(write.mock.calls.map(([chunk]) => chunk.toString()).join('')).toBe(expected);
      expect(errorWrite).not.toHaveBeenCalled();
    } finally {
      write.mockRestore();
      errorWrite.mockRestore();
      stdout.destroy();
      stderr.destroy();
    }
  });
});
