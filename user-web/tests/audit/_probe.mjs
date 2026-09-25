import { chromium } from 'playwright'
const BASE='http://localhost:8211'
const b=await chromium.launch({headless:true})
const ctx=await b.newContext()
const page=await ctx.newPage()
console.log('goto login')
await page.goto(`${BASE}/#/login`,{waitUntil:'domcontentloaded',timeout:15000}).catch(e=>console.log('login goto err',e.message))
await page.waitForSelector('.login-box',{timeout:10000}).then(()=>console.log('login box ok')).catch(e=>console.log('no login box',e.message))
await page.locator('.login-box input[type="text"]').first().fill('admin')
await page.locator('.login-box input[type="password"]').fill('Admin@123456')
await page.locator('.login-box button.el-button--primary').click()
await page.waitForTimeout(3000)
console.log('after login url=',page.url())
await page.goto(`${BASE}/#/knowledgeBase/list`,{waitUntil:'domcontentloaded',timeout:15000}).catch(()=>{})
await page.waitForTimeout(3000)
const info=await page.evaluate(()=>({
  tables:document.querySelectorAll('table').length,
  elTables:document.querySelectorAll('.el-table').length,
  rows:document.querySelectorAll('.el-table__row, tbody tr').length,
  sample:document.body.innerText.slice(0,300)
}))
console.log(JSON.stringify(info,null,2))
await b.close()
