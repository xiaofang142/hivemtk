const { chromium } = require('playwright');
const TOKEN = process.argv[2];
const ROUTES = ['/sms/list','/sms/config','/sopAgent/list','/sop-template/list','/system/config','/system/monitor','/system/material-library','/system/obs-config','/dashboard/drafts','/douyinCard'];
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.addInitScript(t => {
    localStorage.setItem('token', t);
    localStorage.setItem('user_info', JSON.stringify({id:26,username:'uit_admin',email:'',role:'admin'}));
  }, TOKEN);
  page.on('response', res => {
    const s = res.status();
    const url = res.url();
    if (url.includes('/api/') && s >= 400) console.log('FAIL', s, url);
  });
  for (const r of ROUTES) {
    console.log('\n===', r, '===');
    try { await page.goto('http://localhost:8211/#' + r, { waitUntil: 'domcontentloaded', timeout: 15000 }); } catch(e) { console.log('  goto err', e.message.slice(0,120)); }
    await page.waitForTimeout(4000);
    const info = await page.evaluate(() => ({
      url: location.href,
      text: document.body.innerText.replace(/\s+/g,' ').slice(0,250),
    }));
    console.log('  URL:', info.url);
    console.log('  TXT:', info.text);
  }
  await browser.close();
})();
