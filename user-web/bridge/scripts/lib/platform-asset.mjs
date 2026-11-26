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
//   头：优先 Authorization: Bearer <localStorage.token>；2026-11-26 起平台后台
//       改为 HttpOnly cookie 会话（localStorage.token 为空），此时用 same-origin
//       fetch 的 credentials:'include' 携带 cookie，实测列表/详情接口 200。
//       X-Contract-Version: 1 可选，实测带与不带都 200。
//   成功返回 {code:200, msg:'安装包上传成功，请核对内容并审核上架',
//            data:{asset_id, file_sha256, file_url, filename, name,
//                  size_bytes, status:'pending', version}}
//   注意：该接口对同名资产会**新建一条 asset**（不会给旧资产加版本）。
//
// 审核上架接口：先从 admin 列表按 asset_id 解析数据库 id，再读取 admin
// detail 得到当前 review_identity，最后 POST batch-publish 的 asset_ids +
// targets[{id,asset_id,review_identity}]。不得回退到 ID-only 请求。
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
      const hasSession = t.length > 20 || (/资产市场/.test(body) && !/^登录/.test(body.trim()));
      return { hasToken: t.length > 20, hasSession, loginPage: /登录/.test(body.slice(0, 200)) && !/资产市场/.test(body), tokenLen: t.length, body: body.slice(0, 160) };
    });
    if (!session.hasSession) { await ts.finish({ keep: [] }); return fail('not-logged-in', { session }); }
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
        headers: { ...(token ? { Authorization: 'Bearer ' + token } : {}), 'X-Contract-Version': '1' },
        credentials: 'include',
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

    // 管理员上传现在进入 pending。读取后台审核列表与详情，确认详情仍
    // 指向刚上传的版本，再携带该快照 identity 调 batch-publish。
    let publishRes = null;
    if (DO_PUBLISH) {
      if (!assetId) {
        await ts.finish({ keep: [] });
        return fail('review-snapshot-failed', { error: 'upload response has no asset_id' });
      }
      const review = await p.evaluate(async (arg) => {
        const [id, expectedVersion, expectedSHA256] = arg;
        const token = localStorage.getItem('token') || '';
        const headers = { ...(token ? { Authorization: 'Bearer ' + token } : {}), 'X-Contract-Version': '1' };
        const listResponse = await fetch('/platform/asset-market/assets?status=pending&page=1&size=100', { headers, credentials: 'include' });
        const listText = await listResponse.text();
        let listEnvelope = null;
        try { listEnvelope = JSON.parse(listText); } catch { /* 保持原文 */ }
        const rows = listEnvelope && listEnvelope.data && listEnvelope.data.list;
        if (listResponse.status !== 200 || !listEnvelope || listEnvelope.code !== 200 || !Array.isArray(rows)) {
          return { ok: false, step: 'review-list-failed', status: listResponse.status, code: listEnvelope && listEnvelope.code, raw: listText.slice(0, 600) };
        }
        const row = rows.find((item) => item.asset_id === id);
        if (!row || !Number.isInteger(row.id) || row.id <= 0) {
          return { ok: false, step: 'review-row-not-found', error: 'uploaded asset was not present in pending review list' };
        }
        const detailResponse = await fetch('/platform/asset-market/assets/' + encodeURIComponent(row.id), { headers, credentials: 'include' });
        const detailText = await detailResponse.text();
        let detailEnvelope = null;
        try { detailEnvelope = JSON.parse(detailText); } catch { /* 保持原文 */ }
        const detail = detailEnvelope && detailEnvelope.data;
        const identity = detail && detail.review_identity;
        if (detailResponse.status !== 200 || !detailEnvelope || detailEnvelope.code !== 200 ||
            !detail || detail.id !== row.id || detail.asset_id !== id || !identity ||
            identity.status !== 'pending' || !Number.isInteger(identity.version_id) || identity.version_id <= 0 ||
            identity.sha256 !== expectedSHA256 || identity.revision < 1 ||
            !detail.version_info || detail.version_info.version !== expectedVersion) {
          return { ok: false, step: 'review-snapshot-failed', status: detailResponse.status, code: detailEnvelope && detailEnvelope.code, raw: detailText.slice(0, 600) };
        }
        return { ok: true, target: { id: detail.id, asset_id: id, review_identity: identity } };
      }, [assetId, u.data.version, u.data.file_sha256]);
      if (!review.ok) {
        await ts.finish({ keep: [] });
        return fail(review.step || 'review-snapshot-failed', { upload: { status: upload.status, code: u.code, msg: u.msg }, review });
      }
      const pub = await p.evaluate(async (target) => {
        const token = localStorage.getItem('token') || '';
        const r = await fetch('/platform/asset-market/assets/batch-publish', {
          method: 'POST',
          headers: {
            ...(token ? { Authorization: 'Bearer ' + token } : {}),
            'X-Contract-Version': '1',
            'Content-Type': 'application/json',
          },
          credentials: 'include',
          body: JSON.stringify({ asset_ids: [target.asset_id], targets: [target] }),
        });
        const text = await r.text();
        let json = null;
        try { json = JSON.parse(text); } catch { /* 保持原文 */ }
        return { status: r.status, json, raw: text.slice(0, 600) };
      }, review.target);
      publishRes = pub;
      if (pub.status !== 200 || !pub.json || pub.json.code !== 200 || !pub.json.data || pub.json.data.approved !== 1) {
        await ts.finish({ keep: [] });
        return fail('publish-failed', { publish: { status: pub.status, code: pub.json && pub.json.code, msg: pub.json && pub.json.msg, raw: pub.raw } });
      }
    }
    await ts.finish({ keep: [] });
    write({
      ok: true,
      assetId,
      fileUrl: u.data.file_url,
      status: publishRes ? 'approved' : u.data.status,
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
