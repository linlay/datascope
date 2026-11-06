// Isolated UI/API integration via TestWorkbenchBrowserHarness; no user data.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { resolve } = require('node:path');
const { mkdir } = require('node:fs/promises');
const assert = require('node:assert/strict');
const root = resolve(__dirname, '..');
const base = 'http://127.0.0.1:18092';
const output = process.env.DATASCOPE_LOGIN_SCREENSHOTS;
const server = spawn(process.execPath, [resolve(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '18092'], {
 cwd: resolve(root, 'web'), env: { ...process.env, ADMIN_API_URL: 'http://127.0.0.1:18091' }, stdio: 'ignore',
});
(async () => {
 let browser, page;
 try {
  for (let i=0; i<100; i++) { try { if ((await fetch(base)).ok) break; } catch {} await new Promise(r=>setTimeout(r,100)); }
  if(output) await mkdir(output,{recursive:true});
  browser = await chromium.launch({headless:true,executablePath:process.env.DATASCOPE_CHROMIUM});
  const admin = await browser.newContext();
  assert.equal((await admin.request.post(base+'/api/auth/login',{data:{username:'admin',password:'admin-test-password'}})).status(),200);
  const db=await (await admin.request.get(base+'/api/databases/default')).json();
  assert.equal((await admin.request.put(base+'/api/databases/default',{data:{...db,discovery:undefined,authentication:{type:'local',source:'GLOBAL',access:{id:'login',name:'Login test',mode:'ACCOUNT',provider_id:'business-account',permissions:['query']}}}})).status(),200);
  const context = await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write']});
  page = await context.newPage(); page.setDefaultTimeout(12000);
  const errors=[],requests=[]; page.on('pageerror',e=>errors.push(e.message)); page.on('request',r=>requests.push(r.url()));
  const url=base+'/#/business-login?database=default';
  await page.goto(url);
  await page.getByRole('button',{name:'登录并获取 Token',exact:true}).waitFor();
  assert.ok(!requests.some(url=>url.includes('/api/auth/')),'public login used platform auth');
  await page.getByLabel('用户名',{exact:true}).fill('E1');
  await page.getByLabel('密码',{exact:true}).fill('wrong-password');
  await page.getByRole('button',{name:'登录并获取 Token',exact:true}).click();
  await page.getByText('账号或密码错误',{exact:true}).waitFor();
  assert.equal(await page.getByLabel('密码',{exact:true}).inputValue(),'');
  await page.getByLabel('密码',{exact:true}).fill('business-password');
  await page.getByRole('button',{name:'登录并获取 Token',exact:true}).click();
  await page.getByText('登录成功',{exact:true}).waitFor();
  const token=await page.getByLabel('个人 Token',{exact:true}).inputValue();
  assert.ok(token.length>100);
  const reply=await context.request.post('http://127.0.0.1:18090/mcp/default',{headers:{Authorization:'Bearer '+token,Accept:'application/json, text/event-stream'},data:{jsonrpc:'2.0',id:1,method:'initialize',params:{protocolVersion:'2025-03-26',capabilities:{},clientInfo:{name:'login-ui-test',version:'1'}}}});
  assert.equal(reply.status(),200);
  await page.getByRole('button',{name:'复制 Token',exact:true}).click();
  assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),token);
  const storage=await page.evaluate(()=>JSON.stringify({local:{...localStorage},session:{...sessionStorage},url:location.href}));
  assert.ok(!storage.includes(token),'token persisted outside page memory');
  if(output) await page.screenshot({path:resolve(output,'business-login-desktop.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  if(output) await page.screenshot({path:resolve(output,'business-login-mobile.png'),fullPage:true});
  await page.getByRole('button',{name:'清除并重新登录',exact:true}).click();
  await page.getByRole('button',{name:'登录并获取 Token',exact:true}).waitFor();
  assert.equal(await page.getByLabel('个人 Token',{exact:true}).count(),0);
  // A short UI lifetime lets this test verify expiry without waiting one hour.
  await page.route('**/auth/databases/default/token',async route=>{
   const response=await route.fetch(); const body=await response.json();
   await route.fulfill({response,json:{...body,expires_in:2}});
  });
  await page.getByLabel('密码',{exact:true}).fill('business-password');
  await page.getByRole('button',{name:'登录并获取 Token',exact:true}).click();
  await page.getByText('登录成功',{exact:true}).waitFor();
  await page.getByText('Token 已过期，请重新登录',{exact:true}).waitFor();
  assert.equal(await page.getByLabel('个人 Token',{exact:true}).count(),0);
  await page.unroute('**/auth/databases/default/token');
  await page.getByLabel('密码',{exact:true}).fill('business-password');
  await page.getByRole('button',{name:'登录并获取 Token',exact:true}).click();
  await page.getByText('登录成功',{exact:true}).waitFor();
  await page.reload(); await page.getByRole('button',{name:'登录并获取 Token',exact:true}).waitFor();
  assert.equal(await page.getByLabel('个人 Token',{exact:true}).count(),0);
  await page.goto(base+'/#/business-login?database=missing');
  await page.getByText('数据库不存在或 MCP 未开启',{exact:true}).waitFor();
  assert.equal(await page.getByRole('button',{name:'登录并获取 Token',exact:true}).count(),0);
  assert.deepEqual(errors,[]);
  console.log('PASS public business login, bad credentials, Bearer MCP, copy, no persistent token, clear/reload/expiry, mobile and missing database');
 } catch(error) { if(page) console.error(await page.locator('body').innerText()); throw error; } finally { if(browser)await browser.close(); server.kill('SIGTERM'); }
})().catch(error=>{console.error(error);process.exitCode=1;});
