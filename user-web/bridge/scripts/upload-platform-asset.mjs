#!/usr/bin/env node
// 链 C 执行器：把发行包上传到平台并上架为 browser_extension 资产。
//
// 为什么需要它：扩展不上架 Chrome Web Store，租户从用户端「桥接扩展安装包」页
// 下载 zip；那条下载链读平台 asset（类型 browser_extension），所以发行包必须先
// 上到平台并处于「已上架」状态。
//
// 实现方式：在 ego-browser 已登录的平台后台页面上下文里直调平台 API
//   POST /platform/asset-market/file-assets（multipart）
//   POST /platform/asset-market/assets/batch-publish（上架）
// 为什么不点后台 UI：平台后台「上传并上架」按钮本身是坏的（前端 bundle 里
// 用裸 axios 实例调 .upload()，而 multipart 能力在 request.upload 上）——
// 点它弹「e.upload is not a function」且零网络请求，真人点也一样。详见
// scripts/lib/platform-asset.mjs 头部注释与 docs/operations/RELEASE-PIPELINE.md。
//
// 用法：
//   node scripts/upload-platform-asset.mjs                      # 默认 zip=release/hivebridge-<版本>.zip
//   node scripts/upload-platform-asset.mjs --zip release/hivebridge-1.3.0.zip
//   node scripts/upload-platform-asset.mjs --dry-run             # 只校验登录态与包，不发请求
//   node scripts/upload-platform-asset.mjs --no-verify           # 跳过用户端页面回验
//   node scripts/upload-platform-asset.mjs --no-publish          # 只上传不调上架
//
// 前置条件：ego-browser 里 manage.xapptool.cn 已是平台运营/管理员登录态
//          （复用用户登录态，见 AGENTS.md 浏览器规则；未登录时脚本以
//           step=not-logged-in 明确失败，不会误发请求）。
import { readFileSync, existsSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { uploadExtensionAsset, verifyTenantExtensionPage } from './lib/platform-asset.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const argValue = (flag) => {
  const i = args.indexOf(flag);
  return i >= 0 && args[i + 1] && !args[i + 1].startsWith('--') ? args[i + 1] : null;
};
const opt = {
  zip: argValue('--zip'),
  name: argValue('--name'),
  industry: argValue('--industry'),
  version: argValue('--version'),
  notes: argValue('--notes'),
  dryRun: args.includes('--dry-run'),
  noVerify: args.includes('--no-verify'),
  noPublish: args.includes('--no-publish'),
};

const log = (s) => console.log(s);
const fail = (s) => {
  console.error(`\n❌ ${s}\n`);
  process.exit(1);
};

const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
const version = opt.version || pkg.version;
const zipPath = resolve(root, opt.zip || `release/hivebridge-${version}.zip`);
const name = opt.name || 'HiveBridge 蜂桥';
const industry = opt.industry || 'general';
const notes = opt.notes || `v${version} 发行构建（详见 RELEASE_NOTES.md）`;

log(`\n=== 链 C：上传扩展包到平台（直调平台 API）===`);
log(`版本     ${version}`);
log(`资产名   ${name}`);
log(`行业     ${industry}`);
log(`zip      ${zipPath}`);
if (opt.dryRun) log(`模式     --dry-run（只校验登录态与包，不发请求）`);

if (!existsSync(zipPath)) {
  fail(`找不到发行包 ${zipPath}\n先生成：BRIDGE_ONLINE_SERVER=https://workapi.xapptool.cn npm run release`);
}
const size = readFileSync(zipPath).length;
log(`大小     ${size} 字节`);

const up = uploadExtensionAsset({
  zipPath, name, industry, version, notes,
  dryRun: opt.dryRun, publish: !opt.noPublish,
});
if (!up.ok) {
  const detail = up.error || (up.upload ? JSON.stringify(up.upload).slice(0, 400) : '');
  fail(`上传失败（step=${up.step}${detail ? `: ${detail}` : ''}）`);
}
log(`✅ 平台上传成功${opt.dryRun ? '（dry-run，未提交）' : ''}`);
if (up.assetId) log(`   资产 ID  ${up.assetId}`);
if (up.fileUrl) log(`   文件 URL ${up.fileUrl}`);
if (up.status) log(`   资产状态 ${up.status}`);
if (up.publish) log(`   上架接口 ${up.publish.status} ${JSON.stringify(up.publish.json || up.publish.raw).slice(0, 200)}`);

if (opt.dryRun) {
  log(`\n=== dry-run 完成：登录态正常即可去掉 --dry-run 真提交 ===\n`);
  process.exit(0);
}

if (opt.noVerify) {
  log(`\n=== 完成（--no-verify，未回验用户端页面）===\n`);
  process.exit(0);
}

log(`\n=== 回验用户端「桥接扩展安装包」页是否显示 v${version} ===`);
const v = verifyTenantExtensionPage({ version });
if (!v.ok) {
  fail(`用户端页面未显示 v${version}（step=${v.step || '-'}${v.error ? `: ${v.error}` : ''}）\n页面摘要：${String(v.text || '').slice(0, 300)}\n\n可能原因：平台侧缓存/异步，稍后重试；或用户端登录态失效。`);
}
log(`✅ 用户端已显示 v${version}，租户可下载`);

log(`\n=== 链 C 完成 ===\n`);
log(`后续（可选）：node scripts/install-extension.mjs --download   # 从用户端页下载并装入本机 Chrome\n`);
