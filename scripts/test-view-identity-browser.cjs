// Browser regression with mocked platform APIs; never accesses a business database.
// Run with DATASCOPE_PLAYWRIGHT pointing to an existing Playwright installation.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const assert = require('node:assert/strict');
(async () => {
 const root = resolve(__dirname, '..'), base = 'http://127.0.0.1:18195';
 const vite = spawn(process.execPath, [resolve(root, 'web/node_modules/vite/bin/vite.js'), '--port', '18195', '--strictPort'], { cwd: resolve(root, 'web'), stdio: 'ignore' });
 let browser;
 try {
  for (let i = 0; i < 80; i++) { try { if ((await fetch(base)).ok) break; } catch {} await new Promise(r => setTimeout(r, 100)); }
  browser = await chromium.launch({ headless: true, executablePath: process.env.DATASCOPE_CHROMIUM });
  const page = await browser.newPage({ viewport: { width: 1500, height: 1000 } });
  page.setDefaultTimeout(10000);
  const errors = [], calls = []; let admin = true;
  page.on('pageerror', e => errors.push(e.message));
  const runtime = readFileSync(resolve(root, 'internal/view/templates/runtime.js'), 'utf8');
  const html = `<script>${runtime}</script><button id="query">Query</button><button id="trial">Trial</button><button id="receipt">Receipt</button><pre id="out"></pre><script>
   datascope.onData(()=>{document.body.dataset.ready='true'});
   const out=document.getElementById('out'); const show=async(name,args)=>{try{out.textContent=JSON.stringify(await datascope.callTool(name,args))}catch(e){out.textContent=e.message}};
   document.getElementById('query').onclick=()=>show('query',{sql:'SELECT 1',user_id:999});
   document.getElementById('trial').onclick=()=>show('invoke_action',{name:'example',request_id:'test-request',arguments:{value:1},business_user_id:999,database_id:'other'});
   document.getElementById('receipt').onclick=()=>show('invoke_action',{name:'example',request_id:'test-request',result_only:true});
  </script>`;
  const db = { id: 'default', name: 'Test', driver: 'sqlite', path: 'test', revision: 1, permissions: ['config.read','view.write','query','action.write'], business_user_id: 1, config: { schemes: [], tables: [] }, views: [{ id: 'test', name: 'Test view', html, published: true }] };
  await page.route('**/api/**', async route => {
   const path = new URL(route.request().url()).pathname;
   const body = route.request().postDataJSON();
   calls.push({ path, body }); let data = {};
   if (path === '/api/auth/me') data = { id: 1, username: 'admin', system_admin: admin };
   else if (path === '/api/auth/method') data = { mode: 'ACCOUNT' };
   else if (path === '/api/databases') data = [db];
   else if (path.endsWith('/employees')) data = [{ id:1,emp_no:'E1',name:'Alice',status:'active' },{ id:2,emp_no:'E2',name:'Bob',status:'active' }];
   else if (path.endsWith('/rewrite')) data = { identity: body.user_id, columns: ['id'], rows: [{id:body.user_id}] };
   else if (path === '/api/management/call') { assert.equal(body.action, 'try_action'); data = {status:'preview',preview:{writes:[]}}; }
   else if (path.endsWith('/business-mcp')) data = {mode:'OAUTH',provider_type:'DEMO_OAUTH'};
   await route.fulfill({ json: data });
  });
  await page.goto(`${base}/#/view-workbench/test?database=default&mode=identity&identity=1`);
  const frame = page.frameLocator('iframe');
  await frame.locator('body[data-ready=true]').waitFor();
  // Allow the development StrictMode frame remount and bridge handshake to settle.
  await page.waitForTimeout(300);
  await frame.locator('#query').click();
  await frame.locator('#out').filter({hasText:'"identity":1'}).waitFor();
  await frame.locator('#trial').click();
  await frame.locator('#out').filter({hasText:'"status":"preview"'}).waitFor();
  const trial = calls.find(c => c.body?.action === 'try_action');
  assert.equal(trial.body.arguments.business_user_id,1);
  assert.equal(trial.body.arguments.database_id,'default');
  await frame.locator('#receipt').click();
  await page.waitForTimeout(100);
  assert.equal(calls.filter(c => c.body?.action === 'try_action').length,1);
  // Change identity using the actual picker, then verify old data and receipts are gone.
  await page.locator('.view-workbench .ant-select').last().click();
  await page.locator('.ant-select-item-option').filter({hasText:'E2 Bob'}).last().click();
  await frame.locator('#out').filter({hasText:/^$/}).waitFor({state:'attached'});
  await frame.locator('body[data-ready=true]').waitFor();
  // Allow the development StrictMode frame remount and bridge handshake to settle.
  await page.waitForTimeout(300);
  await frame.locator('#query').click();
  await frame.locator('#out').filter({hasText:'"identity":2'}).waitFor();
  await frame.locator('#receipt').click();
  await frame.locator('#out').filter({hasText:'当前预览没有此请求的回执'}).waitFor();
  assert.equal(calls.filter(c => c.path.endsWith('/business-mcp')).length,0);
  await page.getByRole('tab',{name:'真实业务预览',exact:true}).click();
  await page.getByRole('button',{name:'使用业务账号 OAuth 登录',exact:true}).waitFor();
  assert.equal(await page.locator('.ant-alert-warning').count(),0);
  await page.getByRole('button',{name:'真实业务预览说明',exact:true}).hover();
  await page.getByRole('tooltip').filter({hasText:'使用本人业务认证'}).waitFor();
  admin = false;
  await page.reload();
  await page.getByRole('tab',{name:'身份预览',exact:true}).waitFor();
  assert.equal(await page.getByRole('tab',{name:'身份预览',exact:true}).getAttribute('aria-disabled'),'true');
  assert.equal(await page.locator('iframe').count(),0);
  assert.deepEqual(errors,[]);
  console.log('PASS identity preview, trusted identity, rollback-only action, cached receipt, identity reset, business login, tooltip, permission gate');
 } finally { await browser?.close(); vite.kill(); }
})().catch(e => { console.error(e); process.exitCode=1; });
