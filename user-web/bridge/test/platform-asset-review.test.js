// @vitest-environment node
// Contract tests execute the generated upload script against mocked page APIs;
// they never launch ego-browser or access a live website.
import { describe, expect, it, vi } from 'vitest';
import { mkdtempSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const calls = vi.hoisted(() => ({ runtime: null }));
vi.mock('node:child_process', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    execFileSync: vi.fn((_command, args) => {
      const script = args.at(-1);
      const runtime = calls.runtime;
      const wrapper = `
        const fs = require('node:fs');
        const RESULT = ${JSON.stringify(runtime.resultPath)};
        const REQUESTS = ${JSON.stringify(runtime.requestPath)};
        const FAILURE = ${JSON.stringify(runtime.failAt || '')};
        globalThis.localStorage = { getItem: () => 'synthetic-admin-token' };
        globalThis.document = { body: { innerText: '资产市场' } };
        globalThis.fetch = async (url, init = {}) => {
          const path = String(url);
          const method = init.method || 'GET';
          const request = { path, method, body: init.body || null };
          const requests = fs.existsSync(REQUESTS) ? JSON.parse(fs.readFileSync(REQUESTS, 'utf8')) : [];
          requests.push(request);
          fs.writeFileSync(REQUESTS, JSON.stringify(requests));
          let response;
          if (path.includes('/file-assets')) {
            response = { code: 200, data: { asset_id: 'ast-synthetic-1', status: 'pending', file_url: '/synthetic/file.zip', file_sha256: '${'a'.repeat(64)}', version: '1.0.0', size_bytes: 7 } };
          } else if (FAILURE === 'detail' && path.endsWith('/assets/46')) {
            response = { code: 409, msg: 'snapshot unavailable' };
          } else if (path.includes('/assets?')) {
            response = { code: 200, data: { list: [{ id: 46, asset_id: 'ast-synthetic-1' }] } };
          } else if (path.endsWith('/assets/46')) {
            response = { code: 200, data: { id: 46, asset_id: 'ast-synthetic-1', version_info: { version: '1.0.0' }, review_identity: { version_id: 88, sha256: '${'a'.repeat(64)}', status: 'pending', revision: 3 } } };
          } else if (path.endsWith('/batch-publish')) {
            const body = JSON.parse(init.body || '{}');
            response = FAILURE === 'publish'
              ? { code: 409, msg: 'stale review snapshot' }
              : body.targets?.[0]?.review_identity
              ? { code: 200, data: { approved: 1 } }
              : { code: 400, msg: 'missing review target' };
          } else {
            response = { code: 404, msg: 'unexpected route' };
          }
          return { status: response.code === 200 ? 200 : response.code, text: async () => JSON.stringify(response) };
        };
        globalThis.taskSpace = async () => ({
          page: async () => ({ goto: async () => {}, waitForTimeout: async () => {}, evaluate: async (fn, arg) => fn(arg) }),
          finish: async () => {},
        });
        (async () => { await import('data:text/javascript;base64,' + Buffer.from(${JSON.stringify(script)}, 'utf8').toString('base64')); })()
          .catch((error) => { fs.writeFileSync(RESULT, JSON.stringify({ ok: false, step: 'runtime', error: String(error) })); process.exitCode = 2; });
      `;
      const run = actual.spawnSync(process.execPath, ['-e', wrapper], { encoding: 'utf8' });
      if (run.status !== 0) throw new Error(run.stderr || `mocked browser script exited ${run.status}`);
    }),
  };
});

import { uploadExtensionAsset } from '../scripts/lib/platform-asset.mjs';

function runUpload(failAt = '') {
  const directory = mkdtempSync(join(tmpdir(), 'fa16-bridge-contract-'));
  const zipPath = join(directory, 'synthetic.zip');
  const resultPath = join(directory, 'result.json');
  const requestPath = join(directory, 'requests.json');
  writeFileSync(zipPath, 'synthetic zip bytes');
  calls.runtime = { resultPath, requestPath, failAt };
  const result = uploadExtensionAsset({ zipPath, name: 'synthetic', version: '1.0.0', resultPath });
  return {
    result,
    requests: existsSync(requestPath) ? JSON.parse(readFileSync(requestPath, 'utf8')) : [],
  };
}

describe('FA-16 release upload/publish contract', () => {
  it('loads the administrator snapshot and publishes its exact target while preserving result fields', () => {
    const { result, requests } = runUpload();
    expect(result.ok, JSON.stringify(result)).toBe(true);
    expect(result.assetId).toBe('ast-synthetic-1');
    expect(result.fileUrl).toBe('/synthetic/file.zip');
    expect(result.version).toBe('1.0.0');
    expect(result.sizeBytes).toBe(7);
    expect(requests.map(({ path, method }) => [path, method])).toEqual([
      ['/platform/asset-market/file-assets', 'POST'],
      ['/platform/asset-market/assets?status=pending&page=1&size=100', 'GET'],
      ['/platform/asset-market/assets/46', 'GET'],
      ['/platform/asset-market/assets/batch-publish', 'POST'],
    ]);
    const publish = JSON.parse(requests.at(-1).body);
    expect(publish).toEqual({
      asset_ids: ['ast-synthetic-1'],
      targets: [{ id: 46, asset_id: 'ast-synthetic-1', review_identity: { version_id: 88, sha256: 'a'.repeat(64), status: 'pending', revision: 3 } }],
    });
    expect(result.publish.status).toBe(200);
  });

  it('reports approved only after batch publish confirms approval', () => {
    const { result } = runUpload();
    expect(result.status).toBe('approved');
  });

  it('propagates snapshot failure and never claims a successful publish', () => {
    const { result, requests } = runUpload('detail');
    expect(result.ok).toBe(false);
    expect(result.step).toBe('review-snapshot-failed');
    expect(requests.some(({ path }) => path.endsWith('/batch-publish'))).toBe(false);
  });

  it('propagates a rejected version-bound publish response', () => {
    const { result, requests } = runUpload('publish');
    expect(result.ok).toBe(false);
    expect(result.step).toBe('publish-failed');
    expect(result.publish.status).toBe(409);
    expect(JSON.parse(requests.at(-1).body).targets[0].review_identity.version_id).toBe(88);
  });

  it('preserves no-publish mode and its result shape', () => {
    const directory = mkdtempSync(join(tmpdir(), 'fa16-bridge-no-publish-'));
    const zipPath = join(directory, 'synthetic.zip');
    const resultPath = join(directory, 'result.json');
    const requestPath = join(directory, 'requests.json');
    writeFileSync(zipPath, 'synthetic zip bytes');
    calls.runtime = { resultPath, requestPath, failAt: '' };
    const result = uploadExtensionAsset({ zipPath, name: 'synthetic', version: '1.0.0', resultPath, publish: false });
    const requests = JSON.parse(readFileSync(requestPath, 'utf8'));
    expect(result.ok).toBe(true);
    expect(result.status).toBe('pending');
    expect(result.publish).toBe(null);
    expect(requests.map(({ path }) => path)).toEqual(['/platform/asset-market/file-assets']);
  });
});
