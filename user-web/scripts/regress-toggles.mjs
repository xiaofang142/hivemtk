import { chromium } from 'playwright';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }
const BASE='http://localhost:8211';
const b=await chromium.launch({headless:true});
const ctx=await b.newContext({viewport:{width:1440,height:900}});
const page=await ctx.newPage();
const apiErrs=[],conErrs=[];
page.on('response',r=>{if(r.url().includes('/api/')&&r.status()>=400)apiErrs.push(r.status()+' '+r.url().replace(BASE,''))});
page.on('console',m=>{if(m.type()==='error')conErrs.push(m.text().substring(0,200))});
await page.goto(BASE+'/#/login',{waitUntil:'networkidle',timeout:15000});
await page.waitForTimeout(2000);
const u=await page.$('input[type="text"], input[placeholder*="用户名"], input[placeholder*="邮箱"]');
const p=await page.$('input[type="password"]');
await u.fill('admin');await p.fill(ADMIN_PASSWORD);
const btn=await page.$('button[type="submit"], .el-button--primary');
if(btn)await btn.click();
await page.waitForTimeout(5000);
const routes=['/faq/list','/sop-template/list','/kuaishouCard'];
for(const route of routes){
  apiErrs.length=0;conErrs.length=0;
  await page.goto(BASE+'/#/'+route.replace(/^\//,''),{waitUntil:'networkidle',timeout:20000});
  await page.waitForTimeout(2500);
  // 找第一个 el-switch 并点击
  const switches=await page.$$('.el-switch');
  let clicked=false;
  for(const sw of switches){
    if(await sw.isVisible()){await sw.click();clicked=true;break;}
  }
  await page.waitForTimeout(1500);
  console.log(route, clicked?'switch-clicked':'no-switch', 'api:'+apiErrs.length, apiErrs.slice(0,2).join('|'), 'console:'+conErrs.filter(e=>!e.includes('Failed to load resource')&&!e.includes('favicon')).length);
  // 再点回来
  if(clicked){const sws=await page.$$('.el-switch');if(sws[0])await sws[0].click().catch(()=>{});await page.waitForTimeout(800);}
}
await b.close();
console.log('DONE');
