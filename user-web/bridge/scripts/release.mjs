// 本机构建发布脚本（无 CI 依赖，macOS / Linux 直接 `npm run release`）
// 流程：1) 生产构建(esbuild) 2) 打包 dist -> release/hivebridge-<version>.zip 3) 生成发布说明
import { execSync } from 'node:child_process';
import { readFileSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const pkg = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'));
const version = pkg.version;

console.log(`\n🐝 HiveBridge 蜂桥 发布构建 v${version}\n`);

// 0) 版本一致性前置门（必须在构建/清目录之前）
//
// 两件事一起挡：
//   a) package.json 与 manifest.json 版本不一致 —— 打包出来的 zip 名字取 package，
//      扩展真实版本取 manifest，Chrome 里看到的永远是 manifest 的版本；
//   b) 本版没有变更说明 —— 否则发布说明会把上一版的改动说成本版。
//
// 为什么放最前面：变更说明原先在校验在最后（step 3），而 step 2 已经
// `rmSync(release/)` + 打包完成。校验失败时会留下一个「名字是新版本、manifest
// 却是旧版本」的僵尸 zip（2026-11-04 实测：hivebridge-1.0.4.zip 内含 1.0.3）。
// 门放在动文件之前，失败就什么都不动。
const manifest = JSON.parse(readFileSync(resolve(root, 'manifest.json'), 'utf8'));
if (manifest.version !== version) {
  console.error(`\n❌ 版本不一致：package.json=${version} / manifest.json=${manifest.version}`);
  console.error('   请先把两者改成同一个版本再发版（Chrome 读 manifest，zip 名读 package）。\n');
  process.exit(1);
}

// 变更正文按版本号索引，**缺条目直接失败**（同属前置门，必须先于任何文件操作）。
// 历史教训：这段正文原先是硬编码的，发 1.0.2 时标题还写着「v1.0.1 本版变更」，
// 发 1.0.3 时又只改了标题、正文还是上一版内容 —— 两次都把变更说明说错。
// 现在发版前必须为当前 package.json version 补一节，漏了 npm run release 直接红。
const CHANGELOGS = {
  '1.0.4': `- 隐私红线修正：线上地址改为**构建期注入**（BRIDGE_ONLINE_SERVER），源码与开源构建里
  不再出现任何厂商域名。默认构建（自部署）弹窗预填本地 localhost:8204，绝不把客户私信
  指向厂商云端；只有官方 SaaS 发行包在构建时注入云端地址，弹窗才默认线上。
- 弹窗「线上」按钮在未注入时隐藏，DOM 里也不渲染厂商域名；「线上/本地」标签域名字样
  改为运行时填充，开源用户看到的是一份干净的私有化产物。
- 单测补隐私腿：未注入 ⇒ constants.online 为空串、popup 默认=本地、线上按钮隐藏。`,
  '1.0.3': `- 服务端地址默认线上：弹窗「服务端地址」未配置时预填云端地址 https://workapi.xapptool.cn
  （placeholder 同步），从平台下载安装包的租户开箱即连云端，不再需要手敲地址
- 地址两个快捷提示：弹窗新增「线上 / 本地」两个一键填入按钮（线上=workapi.xapptool.cn，
  本地=localhost:8204），自部署用户点「本地」即切回本机地址
- 线上地址单源：新增 constants.ONLINE_USER_SERVER（源 deploy/deploy.conf USER_API_URL 域名族），
  网页端「桥接扩展安装包」安装步骤按部署形态展示地址 + 复制按钮（直接引同一份 constants，
  两处展示永不漂移；SaaS 显示线上地址，私域显示本部署地址，不把云端地址误导给自部署租户）
- 运行期回退语义不变：未配置时 background 的兜底仍是 localhost:8204（DEFAULT_USER_SERVER
  未动），离线自部署的存量行为零影响`,
  '1.0.1': `- 修复同一条 AI 回复被反复重复发送：多个私信标签页同时开着时，下行发送改为按消息加锁串行，
  并在锁内重新读取已发送缓存，其他标签页已发过的消息不再重发（仍会 ack，服务端不会无限重推）
- 修复已发送缓存被互相覆盖：写回前先并入存储里其他标签页的条目，去重记录不再被单个标签页清空
- 目标会话确实不存在时（如客户会话已被平台移除）改为 ack 终态 failed，服务端停止重推；
  页面处于后台不可见状态时不 ack（后台标签页可能渲染不出会话列表，避免误判丢消息）
- 消息抽取：整段聊天线程被误当成「一条消息」时会向下拆成多条气泡（单条长消息不会被拆碎），
  拆不动且超过 4000 字的项按不可信气泡丢弃——此前整段线程会被反复当成新消息抓回并重复回复
- 会话 ID 清洗：剥掉平台 DOM 里混入的 MM/DD 日期串，避免同一会话被判成多个会话`,
};

const currentChangelog = CHANGELOGS[version];
if (!currentChangelog) {
  console.error(`\n❌ package.json 版本是 v${version}，但 scripts/release.mjs 的 CHANGELOGS 里没有这一版。`);
  console.error(`   请先补 v${version} 的变更说明再发版 —— 否则发布说明会把上一版的改动当成本版改动。\n`);
  process.exit(1);
}

// 1) 生产构建
const onlineInjected = process.env.BRIDGE_ONLINE_SERVER || '';
console.log(`==> [1/3] 生产构建 (esbuild)｜线上地址注入：${onlineInjected || '（未注入 → 自部署产物，弹窗默认 localhost:8204）'}`);
execSync('npm run build', { cwd: root, stdio: 'inherit' });

const dist = resolve(root, 'dist');
const releaseDir = resolve(root, 'release');
rmSync(releaseDir, { recursive: true, force: true });
mkdirSync(releaseDir, { recursive: true });

// 2) 打包（zip 根目录即 manifest.json，满足 Chrome 加载要求）
const zipName = `hivebridge-${version}.zip`;
const zipPath = resolve(releaseDir, zipName);
console.log(`==> [2/3] 打包 ${zipName}`);
execSync(`cd "${dist}" && zip -r -X "${zipPath}" . >/dev/null`, { stdio: 'inherit' });

// 3) 发布说明（变更条目已在顶部前置门校验过，这里只做拼装）
const history = Object.keys(CHANGELOGS)
  .filter((v) => v !== version)
  .map((v) => `## 历史变更（v${v}）\n${CHANGELOGS[v]}`)
  .join('\n\n');

const notes = `# HiveBridge 蜂桥 v${version} 发布包

- 产物：\`release/${zipName}\`（解压后含 manifest.json，可直接「加载已解压的扩展程序」）
- 构建时间：${new Date().toISOString()}

## 本版变更（v${version}）
${currentChangelog}

${history}

## 包含能力
- 三平台私信桥接：抖音 / 小红书 / TikTok（网页版）
- 实时上行：客户私信 -> user-server（触发 AI）
- 历史回填：页面加载/会话切换时存量消息仅落库，不触发 AI（防回环）
- 下行回写：user-server AI 回复 -> 回写网页私信输入框并发送
- 拟人限速风控：最小间隔 + 令牌桶 + 会话冷却 + 相同文案去重
- 多用户/多会话：按 (channel, account, conversation) 隔离路由与历史

## 安装（本机 Chrome）
1. 打开 chrome://extensions
2. 开启「开发者模式」
3. 「加载已解压的扩展程序」-> 选择解压后的 dist 目录
4. 点击工具栏图标 -> 填写 user-server 地址（如 http://localhost:8204）-> 保存
5. 打开任一平台私信页 -> 点「自检当前私信页」按 bridge.md §10 真机校准清单核对

## 验证清单
- [ ] 客户发消息后 user-server 收到 inbound_message（AI 触发）
- [ ] AI 回复回写到网页并出现在对话框（outbound_reply）
- [ ] 刷新/切换会话后历史进入 user-server（history 帧，direction=inbound/outbound）
- [ ] 连续高频消息被限速拦截（popup / 后台日志可见 reason）
- [ ] 三平台（抖音 / 小红书 / TikTok / 闲鱼 / 快手）选择器真机校准通过
`;
writeFileSync(resolve(releaseDir, 'RELEASE_NOTES.md'), notes);
console.log(`==> [3/3] 发布说明 -> release/RELEASE_NOTES.md`);
console.log(`\n✅ 完成：release/${zipName}\n`);
