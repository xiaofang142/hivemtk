const { chromium } = require('playwright');
const fs = require('fs');
const path = require('path');
const TOKEN = process.argv[2];
const OUT = process.argv[3] || '/tmp/inventory_report.json';
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
const uniq = routes.filter(r => (seen.has(r.path) ? false : (seen.add(r.path), true)));
const all = [...uniq];
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.addInitScript(t => {
    localStorage.setItem('token', t);
    localStorage.setItem('user_info', JSON.stringify({id:26,username:'uit_admin',email:'',role:'admin'}));
  }, TOKEN);
  const report = [];
  page.on('pageerror', e => { report[report.length-1] && report[report.length-1].pageErrors.push(String(e).slice(0,200)); });
  for (const r of all) {
    if (r.path.includes(':') || r.path.endsWith('?')) continue; // skip param routes
    const row = { mod: r.mod, path: r.path, fails: [], apiBad: [], pageErrors: [] };
    report.push(row);
    const reqs = [];
    const onResp = async res => {
      const s = res.status(); const u = res.url();
      if (u.includes('/api/') && s >= 400) row.fails.push(`${s} ${u}`);
      if (u.includes('/api/') && s >= 200 && s < 400) {
        const ct = res.headers()['content-type'] || '';
        if (ct.includes('json')) { try { const j = await res.json(); if (j && typeof j.code !== 'undefined' && j.code !== 0 && j.code !== '0') row.apiBad.push(`${u} => ${j.code} ${j.message||''}`); } catch(e){ /* 采集失败留空即可 */ } }
      }
    };
    page.on('response', onResp);
    await page.goto('http://localhost:8211/#' + r.path, { waitUntil: 'domcontentloaded', timeout: 15000 }).catch(()=>{});
    await page.waitForTimeout(2200);
    page.off('response', onResp);
    console.log(`${row.fails.length||row.apiBad.length||row.pageErrors.length ? '!!' : 'OK'} ${r.path.padEnd(55)} fails=${row.fails.length} apiBad=${row.apiBad.length} errs=${row.pageErrors.length}`);
  }
  await browser.close();
  fs.writeFileSync(OUT, JSON.stringify(report, null, 2));
  console.log('DONE', report.length);
})();
