// Run through TestSecuritiesViewBrowserHarness against isolated fixture services.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { resolve } = require('node:path');
const { mkdir } = require('node:fs/promises');
const assert = require('node:assert/strict');
(async()=>{
 const root=resolve(__dirname,'../../..'),base='http://127.0.0.1:18192';
 const server=spawn(process.execPath,[resolve(root,'web/node_modules/vite/bin/vite.js'),'--host','127.0.0.1'],{cwd:resolve(root,'web'),env:process.env,stdio:'ignore'});
 let browser;
 try{
  for(let i=0;i<100;i++){try{if((await fetch(base)).ok)break;}catch{}await new Promise(r=>setTimeout(r,100));}
  browser=await chromium.launch({headless:true,executablePath:process.env.DATASCOPE_CHROMIUM});
  const page=await browser.newPage({viewport:{width:1600,height:1100}});page.setDefaultTimeout(20000);
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(`${base}/#/view-workbench/securities_customers?database=default&mode=identity&identity=175`);
  await page.getByLabel('用户名',{exact:true}).fill('admin');await page.getByLabel('密码',{exact:true}).fill('installer-test-password');await page.getByRole('button',{name:/登\s*录/,exact:true}).click();
  let frame=page.frameLocator('iframe');
  await frame.locator('#page-info').filter({hasText:'当前可见客户 8 位'}).waitFor();
  assert.equal(await frame.locator('#results table tr').count(),9);
  if(process.env.DATASCOPE_VIEW_SCREENSHOTS){await mkdir(process.env.DATASCOPE_VIEW_SCREENSHOTS,{recursive:true});await page.locator('iframe').screenshot({path:resolve(process.env.DATASCOPE_VIEW_SCREENSHOTS,'customer-list.png')});}
  await frame.locator('#results').getByRole('button',{name:'客户详情'}).first().click();
  await frame.locator('#customer-detail .metric').nth(5).waitFor();
  assert.equal(await frame.locator('#customer-detail .notice.error').count(),0);
  await frame.locator('#keyword').fill('陈雨桐');await frame.getByRole('button',{name:'查询客户',exact:true}).click();
  await frame.locator('#page-info').filter({hasText:'当前可见客户 1 位'}).waitFor();
  assert.equal(await frame.locator('#results table tr').count(),2);
  await frame.locator('#risk').selectOption('C5');await frame.getByRole('button',{name:'查询客户',exact:true}).click();
  await frame.locator('#page-info').filter({hasText:'当前可见客户 0 位'}).waitFor();
  assert.equal(await frame.locator('#next').isDisabled(),true);
  await page.goto(`${base}/#/view-workbench/securities_rankings?database=default&mode=identity&identity=175`);
  frame=page.frameLocator('iframe');await frame.locator('#results table').waitFor();
  if(process.env.DATASCOPE_VIEW_SCREENSHOTS)await page.locator('iframe').screenshot({path:resolve(process.env.DATASCOPE_VIEW_SCREENSHOTS,'customer-rankings.png')});
  for(const metric of ['cash','holdings','turnover','trade_count','net_deposit','fees']){
   const response=page.waitForResponse(r=>r.url().endsWith('/rewrite')&&r.request().method()==='POST');
   await frame.locator('#metric').selectOption(metric);await response;
   await frame.locator('#filters button:not(:disabled)').waitFor();
   assert.equal(await frame.locator('.notice.error').count(),0,metric);
   assert.equal(await frame.locator('#results table,#results .empty').count(),1,metric);
  }
  await page.goto(`${base}/#/view-workbench/securities_customer?database=default&mode=identity&identity=1`);
  frame=page.frameLocator('iframe');await frame.locator('#customer-number').fill('C00000061');await frame.getByRole('button',{name:'查询客户',exact:true}).click();
  await frame.locator('#customer-detail .metric').nth(5).waitFor();
  assert.match(await frame.locator('#customer-detail').innerText(),/C00000061/);
  for(const name of ['成交','资金流水','持仓']){const response=page.waitForResponse(r=>r.url().endsWith('/rewrite')&&!r.request().postDataJSON().sql.includes('COUNT('));await frame.getByRole('tab',{name,exact:true}).click();await response;await frame.locator('#customer-detail [role=status]').last().filter({hasText:/^$/}).waitFor({state:'attached'});assert.equal(await frame.locator('#customer-detail .notice.error').count(),0);}
  if(process.env.DATASCOPE_VIEW_SCREENSHOTS)await page.locator('iframe').screenshot({path:resolve(process.env.DATASCOPE_VIEW_SCREENSHOTS,'customer-detail.png')});
  assert.deepEqual(errors,[]);
  console.log('PASS customer list/filter/empty state, customer details, seven rankings and manager identity queries');
 }finally{await browser?.close();server.kill();}
})().catch(e=>{console.error(e);process.exitCode=1;});
