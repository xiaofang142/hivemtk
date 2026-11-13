// 平台端「上传扩展包」链（链 C）的 ego-browser 全自动执行模块。
//
// 背景：扩展不上架 Chrome Web Store，租户从用户端「桥接扩展安装包」页下载 zip。
// 那条下载链读的是平台 asset（类型 browser_extension），所以发行包必须先上到
// 平台。
//
// ── 为什么走「页面内直调 API」而不是点平台 UI 表单（2026-11-13 实证）──
// 平台后台「上传扩展包」按钮本身是坏的：点「上传并上架」后弹 toast
// 「e.upload is not a function」，且零网络请求（fetch/XHR 拦截全空）。
// 根因读自平台前端 bundle（platform-web/assets/assetMarket-uW5x-trg.js）：
//   上传函数 f = t => e.upload('/platform/asset-market/file-assets', t)
//   而 request-1P2MNUJn.js 里 multipart 能力在 l.upload（= axios post +
//   Content-Type: multipart/form-data）上，e 是裸 axios 实例 c，没有 .upload。
// ⇒ 平台前端版本错配，**换真人点也一样报错**，与自动化无关。
// 因此本模块绕开 UI，在已登录的后台页面上下文里用 fetch 直调同一接口。
//
// 上传接口（权威）：
//   POST /platform/asset-market/file-assets   multipart/form-data
//   字段：file(blob, filename=zip名) / name / industry / version / changelog
//   头：Authorization: Bearer <localStorage.token>（必需且充分；
//       X-Contract-Version: 1 可选，实测带与不带都 200）
//   成功返回 {code:200, msg:'安装包上传成功，资产已上架',
//            data:{asset_id, file_sha256, file_url, filename, name,
//                  size_bytes, status:'approved', version}}
//   注意：该接口对同名资产会**新建一条 asset**（不会给旧资产加版本）。
//
// 上架接口：POST /platform/asset-market/assets/batch-publish
//   body JSON {"asset_ids": ["ast-…"]}
//   上传后 status 直接是 approved；用户端下载链（user-server
//   bridge_extension.go Latest()）只认已上架的资产，故必须再 publish 一次。
//
// 关键约束（别改）：
//   1. ego-browser 子进程 console.log 透传父终端、execFileSync stdout 捕获为空，
//      状态必须写文件回传（见 lib/ego-browser.mjs 同款说明）。
//   2. 平台后台必须已是登录态（ego-browser 复用用户登录态，见 AGENTS.md 浏览器规则）。
//   3. token 在 localStorage 键 `token`（JWT），从页面里读，不要硬编码。
//   4. manage.xapptool.cn 是 hash 路由 SPA，goto 后要等路由渲染。
import { execFileSync } from 'node:child_process';
import { existsSync, rmSync, readFileSync } from 'node:fs';

const ASSET_LIST_URL = 'https://manage.xapptool.cn/#/platform/asset-market/list';
const TENANT_PAGE_URL = 'https://work.xapptool.cn/#/bridge/extension';

/**
 * 在平台后台上传扩展安装包（直调 API），并上架使其对租户可见。
 *
 * @param {object} o
 * @param {string} o.zipPath      要上传的 zip 绝对路径
 * @param {string} o.name         资产名称，如「HiveBridge 蜂桥」
 * @param {string} o.industry     行业，默认 general
 * @param {string} o.version      版本号
 * @param {string} o.notes        更新说明（changelog）
 * @param {boolean} [o.dryRun]    只做前置校验与页面登录态检查，不发请求
 * @param {boolean} [o.publish]   上传后是否调 batch-publish 上架（默认 true）
 * @param {string} [o.resultPath]
 * @returns {{ok: boolean, step?: string, error?: string, upload?: object, publish?: object}}
 */
export function uploadExtensionAsset({
  zipPath, name, industry = 'general', version, notes = '', dryRun = false,
  publish = true, resultPath = '/tmp/hivebridge-upload-result.json',
}) {
  if (!existsSync(zipPath)) {
    return { ok: false, step: 'zip-missing', error: `${zipPath} 不存在（先 npm run release）` };
  }
  if (existsSync(resultPath)) rmSync(resultPath);
  const zipB64 = readFileSync(zipPath).toString('base64');
  const zipName = zipPath.split('/').pop();
  const script = `
const fs = require('node:fs');
const RESULT = ${JSON.stringify(resultPath)};
const B64 = ${JSON.stringify(zipB64)};
const FNAME = ${JSON.stringify(zipName)};
const NAME = ${JSON.stringify(name)};
const INDUSTRY = ${JSON.stringify(industry)};
const VERSION = ${JSON.stringify(version)};
const NOTES = ${JSON.stringify(notes)};
const DRY = ${dryRun ? 'true' : 'false'};
const DO_PUBLISH = ${publish ? 'true' : 'false'};
const write = (o) => fs.writeFileSync(RESULT, JSON.stringify(o));
const fail = (step, extra) => { write({ ok: false, step, ...(extra || {}) }); return Promise.resolve(); };
(async () => {
  try {
    const ts = await taskSpace('hivebridge-platform-upload');
    const p = await ts.page('p1');
    await p.goto(${JSON.stringify(ASSET_LIST_URL)});
    await p.waitForTimeout(3000);

    // 登录态与 token 校验（不硬编码任何凭据）
    const session = await p.evaluate(() => {
      const t = localStorage.getItem('token') || '';
      const body = document.body ? document.body.innerText.replace(/\\s+/g, ' ') : '';
      return { hasToken: t.length > 20, loginPage: /登录/.test(body.slice(0, 200)) && !/资产市场/.test(body), tokenLen: t.length };
    });
    if (!session.hasToken) { await ts.finish({ keep: [] }); return fail('not-logged-in', { session }); }
    if (DRY) { await ts.finish({ keep: [] }); return write({ ok: true, dryRun: true, session }); }

    // 读 zip → FormData → POST 上传（全部在已登录页面上下文里完成）
    // ⚠️ zip 内容必须由父进程读成 base64 经 evaluate 的 argument 传进来：
    // Chrome 禁止 fetch('file://…')（scheme/CORS 限制，实测 TypeError:
    // Failed to fetch），不能在页面里直接读本地文件。
    const upload = await p.evaluate(async (arg) => {
      const [B64, FNAME, NAME, INDUSTRY, VERSION, NOTES] = arg;
      const token = localStorage.getItem('token') || '';
      const bin = atob(B64);
      const u8 = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
      const blob = new Blob([u8], { type: 'application/zip' });
      const fd = new FormData();
      fd.append('file', blob, FNAME);
      fd.append('name', NAME);
      fd.append('industry', INDUSTRY);
      fd.append('version', VERSION);
      fd.append('changelog', NOTES || ('v' + VERSION + ' 发行构建'));
      const r = await fetch('/platform/asset-market/file-assets', {
        method: 'POST',
        headers: { Authorization: 'Bearer ' + token, 'X-Contract-Version': '1' },
        body: fd,
      });
      const text = await r.text();
      let json = null;
      try { json = JSON.parse(text); } catch { /* 保持原文 */ }
      return { status: r.status, json, raw: text.slice(0, 800) };
    }, [B64, FNAME, NAME, INDUSTRY, VERSION, NOTES]);
    const u = upload.json || {};
    if (upload.status !== 200 || u.code !== 200 || !u.data) {
      await ts.finish({ keep: [] });
      return fail('upload-failed', { upload: { status: upload.status, code: u.code, msg: u.msg, raw: upload.raw } });
    }
    const assetId = u.data.asset_id;

    // 上架：用户端下载链只读已上架资产
    let publishRes = null;
    if (DO_PUBLISH && assetId) {
      const pub = await p.evaluate(async (id) => {
        const token = localStorage.getItem('token') || '';
        const r = await fetch('/platform/asset-market/assets/batch-publish', {
          method: 'POST',
          headers: {
            Authorization: 'Bearer ' + token,
            'X-Contract-Version': '1',
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ asset_ids: [id] }),
        });
        const text = await r.text();
        let json = null;
        try { json = JSON.parse(text); } catch { /* 保持原文 */ }
        return { status: r.status, json, raw: text.slice(0, 600) };
      }, assetId);
      publishRes = pub;
    }
    await ts.finish({ keep: [] });
    write({
      ok: true,
      assetId,
      fileUrl: u.data.file_url,
      status: u.data.status,
      version: u.data.version,
      sizeBytes: u.data.size_bytes,
      publish: publishRes ? { status: publishRes.status, json: publishRes.json, raw: publishRes.raw } : null,
    });
  } catch (e) {
    write({ ok: false, step: 'exception', error: String((e && e.stack) || e) });
  }
})();
`;
  try {
    execFileSync('ego-browser', ['nodejs', '-e', script], { stdio: 'inherit', timeout: 300000 });
  } catch (e) {
    return { ok: false, step: 'ego-browser', error: e.message };
  }
  if (!existsSync(resultPath)) return { ok: false, step: 'no-result' };
  return JSON.parse(readFileSync(resultPath, 'utf8'));
}

/**
 * 核对用户端「桥接扩展安装包」页是否已显示目标版本（链 C 生效验证）。
 *
 * @param {object} o
 * @param {string} o.version
 * @param {string} [o.resultPath]
 */
export function verifyTenantExtensionPage({ version, resultPath = '/tmp/hivebridge-tenant-page.json' }) {
  if (existsSync(resultPath)) rmSync(resultPath);
  const script = `
const fs = require('node:fs');
const RESULT = ${JSON.stringify(resultPath)};
const V = ${JSON.stringify(version)};
const write = (o) => fs.writeFileSync(RESULT, JSON.stringify(o));
(async () => {
  try {
    const ts = await taskSpace('hivebridge-tenant-page');
    const p = await ts.page('p1');
    await p.goto(${JSON.stringify(TENANT_PAGE_URL)});
    await p.waitForTimeout(3200);
    const r = await p.evaluate(() => document.body.innerText.replace(/\\s+/g, ' ').slice(0, 1200));
    const hasVersion = new RegExp('v?' + V.replace(/\\./g, '\\\\.')).test(r);
    const offline = /未接入平台|尚未上架/.test(r);
    write({ ok: hasVersion && !offline, version: V, hasVersion, offline, text: r });
    await ts.finish({ keep: [] });
  } catch (e) {
    write({ ok: false, step: 'exception', error: String((e && e.stack) || e) });
  }
})();
`;
  try {
    execFileSync('ego-browser', ['nodejs', '-e', script], { stdio: 'inherit', timeout: 180000 });
  } catch (e) {
    return { ok: false, step: 'ego-browser', error: e.message };
  }
  if (!existsSync(resultPath)) return { ok: false, step: 'no-result' };
  return JSON.parse(readFileSync(resultPath, 'utf8'));
}
