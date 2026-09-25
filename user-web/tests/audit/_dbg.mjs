import { chromium } from 'playwright'
const BASE='http://localhost:8211'
const b=await chromium.launch({headless:true})
const ctx=await b.newContext()
const page=await ctx.newPage()
await page.goto(`${BASE}/#/login`,{waitUntil:'domcontentloaded',timeout:15000}).catch(()=>{})
await page.waitForSelector('.login-box',{timeout:10000}).catch(()=>{})
await page.locator('.login-box input[type="text"]').first().fill('admin')
await page.locator('.login-box input[type="password"]').fill('Admin@123456')
await page.locator('.login-box button.el-button--primary').click()
await page.waitForTimeout(2500)
await page.goto(`${BASE}/#/knowledgeBase/list`,{waitUntil:'domcontentloaded',timeout:15000}).catch(()=>{})
await page.waitForTimeout(4000)
const dom=await page.evaluate(()=>{
  const t=document.querySelector('.el-table')
  return {
    rows:document.querySelectorAll('.el-table__row').length,
    bodyRows:t?.querySelectorAll('.el-table__body-wrapper tr').length,
    headerText:[...document.querySelectorAll('.el-table__header th')].map(h=>h.innerText.replace(/\s+/g,' ').trim()).slice(0,10),
    firstRowText:document.querySelector('.el-table__row')?.innerText.replace(/\s+/g,' ').trim().slice(0,200)
  }
})
console.log('DOM:',JSON.stringify(dom,null,2))
await b.close()
