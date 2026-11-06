// Run via TestWorkbenchBrowserHarness; never uses the user's configured data.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { resolve } = require('node:path');
const { mkdir } = require('node:fs/promises');
const assert = require('node:assert/strict');
const root = resolve(__dirname, '..');
const base = 'http://127.0.0.1:18092';
const output = process.env.DATASCOPE_AUTH_SCREENSHOTS;
const server = spawn(process.execPath, [resolve(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '18092'], {
 cwd: resolve(root, 'web'), env: { ...process.env, WEB_PORT: '18092', ADMIN_API_URL: 'http://127.0.0.1:18091' }, stdio: 'ignore',
});
(async () => {
 let browser, page;
 try {
  for (let i=0; i<100; i++) { try { if ((await fetch(base)).ok) break; } catch {} await new Promise(r=>setTimeout(r,100)); }
  if (output) await mkdir(output,{recursive:true});
  browser = await chromium.launch({headless:true,executablePath:process.env.DATASCOPE_CHROMIUM});
  const context = await browser.newContext({viewport:{width:1440,height:1050}});
  assert.equal((await context.request.post(base+'/api/auth/login',{data:{username:'admin',password:'admin-test-password'}})).status(),200);
  const getDB = async () => (await context.request.get(base+'/api/databases/default')).json();
  page = await context.newPage(); page.setDefaultTimeout(12000);
  const errors=[]; page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/database');
  const card=page.locator('.ant-card').filter({has:page.locator('.ant-card-head-title').getByText('业务认证',{exact:true})});
  const choice=name=>card.getByRole('radio',{name,exact:true});
  for (const name of ['无需认证（none）','MCP OAuth（mcp）','企业 SSO（oneid）','Token / API Key（token）']) await choice(name).waitFor();
  assert.equal(await choice('MCP OAuth（mcp）').isChecked(),true);
  await choice('企业 SSO（oneid）').check();
  await card.getByRole('textbox',{name:'身份签发方（Issuer）',exact:true}).fill('https://identity.example.com');
  await card.getByRole('textbox',{name:'签名公钥地址（JWKS）',exact:true}).fill('https://identity.example.com/jwks');
  await card.getByRole('textbox',{name:'接收方（Audience）',exact:true}).fill('desktop-client');
  await card.getByRole('button',{name:'保存',exact:true}).click();
  await page.getByText('认证方式已保存，业务客户端需重新连接',{exact:true}).waitFor();
  let db=await getDB(); assert.equal(db.access.policy.mode,'ONEID'); assert.equal(db.access.provider.audience,'desktop-client');
  await page.reload(); await choice('企业 SSO（oneid）').waitFor(); assert.equal(await choice('企业 SSO（oneid）').isChecked(),true);
  if(output)await card.screenshot({path:resolve(output,'business-auth-oneid.png')});
  await choice('Token / API Key（token）').check();
  await card.getByRole('textbox',{name:'API Key 请求头',exact:true}).fill('X-API-Key');
  await card.getByRole('spinbutton',{name:'Token 业务身份',exact:true}).fill('10');
  await card.getByRole('button',{name:'保存',exact:true}).click();
  await page.getByText('认证方式已保存，业务客户端需重新连接',{exact:true}).waitFor();
  db=await getDB(); assert.equal(db.access.policy.mode,'TOKEN'); assert.equal(db.access.provider.header_name,'X-API-Key'); assert.equal(db.access.policy.business_user_id,10);
  await page.reload(); await choice('Token / API Key（token）').waitFor(); assert.equal(await choice('Token / API Key（token）').isChecked(),true);
  if(output)await card.screenshot({path:resolve(output,'business-auth-token.png')});
  const generate=async()=>{
   await card.getByRole('button',{name:'生成或重置 API Key',exact:true}).click();
   const confirmation=page.getByRole('dialog').filter({hasText:'旧 API Key 将立即失效，请同步更新连接器。'});
   await confirmation.getByRole('button',{name:/确\s*定/}).click();
   const issued=page.getByRole('dialog').filter({hasText:'API Key 已生成'});
   await issued.getByLabel('API Key',{exact:true}).waitFor();
   const key=await issued.getByLabel('API Key',{exact:true}).inputValue();
   assert.ok(key.startsWith('ds_'));
   await issued.getByRole('button',{name:'已保存，关闭',exact:true}).click();
   return key;
  };
  const probe=async key=>{
   const response=await context.request.post('http://127.0.0.1:18090/mcp/default',{headers:{'X-API-Key':key,Accept:'application/json, text/event-stream'},data:{jsonrpc:'2.0',id:1,method:'initialize',params:{protocolVersion:'2025-03-26',capabilities:{},clientInfo:{name:'api-key-ui-test',version:'1'}}}});
   return response.status();
  };
  const firstKey=await generate();assert.equal(await probe(firstKey),200);
  const secondKey=await generate();assert.equal(await probe(firstKey),401);assert.equal(await probe(secondKey),200);
  const persisted=await page.evaluate(()=>JSON.stringify({...localStorage,...sessionStorage}));
  assert.ok(!persisted.includes(firstKey)&&!persisted.includes(secondKey));
  await page.reload();await card.getByRole('button',{name:'撤销 API Key',exact:true}).waitFor();
  assert.equal(await page.getByLabel('API Key',{exact:true}).count(),0);
  await card.getByRole('button',{name:'撤销 API Key',exact:true}).click();
  await page.getByRole('dialog').filter({hasText:'撤销后，该密钥不能继续访问本数据库。'}).getByRole('button',{name:/确\s*定/}).click();
  await page.getByText('API Key 已撤销',{exact:true}).waitFor();assert.equal(await probe(secondKey),401);

  await page.setViewportSize({width:720,height:1000});
  if(output)await card.screenshot({path:resolve(output,'business-auth-narrow.png')});
  const width=await page.evaluate(()=>({viewport:innerWidth,body:document.documentElement.scrollWidth}));
  assert.ok(width.body<=width.viewport+1,'page overflows at narrow viewport');
  await choice('无需认证（none）').check();
  await card.getByRole('button',{name:'保存',exact:true}).click();
  await page.getByText('认证方式已保存，业务客户端需重新连接',{exact:true}).waitFor();
  db=await getDB(); assert.equal(db.access.policy.mode,'NONE');
  // Legacy ACCOUNT remains a token sub-type, without being rewritten on load.
  const response=await context.request.post(base+'/api/management/call',{data:{action:'save_database',arguments:{database_id:'default',database:{...db,authentication:{type:'local',source:'GLOBAL',access:{id:'legacy',name:'Legacy account',mode:'ACCOUNT',provider_id:'business-account',permissions:['query']}}}}}});
  assert.equal(response.status(),200);
  await page.reload(); await choice('Token / API Key（token）').waitFor(); assert.equal(await choice('Token / API Key（token）').isChecked(),true);
  assert.equal((await getDB()).access.policy.mode,'ACCOUNT');
  assert.deepEqual(errors,[]);
  console.log('business authentication: four choices, saved OneID/API key, reload, legacy account, narrow layout passed');
 } catch(e) { if(page&&output)await page.screenshot({path:resolve(output,'failure.png'),fullPage:true}); throw e; }
 finally { if(browser)await browser.close(); server.kill('SIGTERM'); }
})().catch(e=>{console.error(e);process.exitCode=1;});
