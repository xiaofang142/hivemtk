const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');

const TOKEN = process.argv[2];
const OUT = process.argv[3] || '/tmp/audit_pages.json';
const modDir = path.join(__dirname, '..', 'src/router/modules');
const routes = [];
for (const f of fs.readdirSync(modDir).filter(x => x.endsWith('.js'))) {
  const txt = fs.readFileSync(path.join(modDir, f), 'utf8');
  const re = /path:\s*['"]([^'"]+)['"]/g; let m;
  while ((m = re.exec(txt)) !== null) {
    let p = m[1]; if (!p.startsWith('/')) p = '/' + p;
    routes.push({ mod: f.replace('.js',''), path: p });
  }
}
const seen = new Set();
const all = routes.filter(r => (seen.has(r.path) ? false : (seen.add(r.path), true)))
  .filter(r => !r.path.includes(':') && !r.path.endsWith('?'));

(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.addInitScript(t => {
    localStorage.setItem('token', t);
    localStorage.setItem('user_info', JSON.stringify({id:26,username:'uit_admin',email:'',role:'admin'}));
  }, TOKEN);

  const report = [];
  for (const r of all) {
    const row = { mod: r.mod, path: r.path, consoleErrors: [], fails: [], toasts: [], text: '' };
    report.push(row);
    const onConsole = msg => {
      const t = msg.type();
      if (t === 'error') row.consoleErrors.push(msg.text().slice(0, 180));
    };
    const onPageErr = e => row.consoleErrors.push('PAGEERR: ' + String(e).slice(0, 180));
    const onResp = res => {
      const s = res.status(); const u = res.url();
      if (u.includes('/api/') && s >= 400) row.fails.push(`${s} ${u.replace('http://localhost:8211','')}`);
    };
    page.on('console', onConsole);
    page.on('pageerror', onPageErr);
    page.on('response', onResp);
    await page.goto('http://localhost:8211/#' + r.path, { waitUntil: 'domcontentloaded', timeout: 15000 }).catch(()=>{});
    await page.waitForTimeout(2200);
    page.off('console', onConsole); page.off('pageerror', onPageErr); page.off('response', onResp);
    try {
      row.toasts = await page.$$eval('.el-message', els => els.map(e => e.innerText.trim()).filter(Boolean));
    } catch(e) { /* 采集失败：字段留空，页面继续审计 */ }
    try {
      row.text = await page.evaluate(() => document.body.innerText);
    } catch(e) { /* 采集失败：字段留空，页面继续审计 */ }
  }
  await browser.close();
  fs.writeFileSync(OUT, JSON.stringify(report));
  console.log('AUDIT_DONE', report.length, 'bytes', fs.statSync(OUT).size);
})();
