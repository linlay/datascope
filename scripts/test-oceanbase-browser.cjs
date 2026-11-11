// Mocked UI acceptance only; no real database connections or writes.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { resolve } = require('node:path');
const assert = require('node:assert/strict');
const root = resolve(__dirname, '..');
const base = 'http://127.0.0.1:18197';
const server = spawn(process.execPath, [resolve(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '18197'], { cwd: resolve(root, 'web'), stdio: 'ignore' });
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
let browser;
(async () => {
 try {
  for (let i=0;i<100;i++) { try { if ((await fetch(base)).ok) break; } catch {} await pause(100); }
  browser = await chromium.launch({headless:true, executablePath:process.env.DATASCOPE_CHROMIUM});
  const context = await browser.newContext({viewport:{width:1440,height:1000},locale:'zh-CN'});
  let db = {id:'ob_ui',name:'OceanBase UI',driver:'oceanbase',revision:1,mcp_enabled:false,connection:{host:'fixture',port:2883,database:'business',schema:'business',allowed_schemas:['business','sales'],username:'reader@tenant#cluster'},config:{version:1,schemes:[],tables:[]},discovery:{tables:[],reports:[],annotations:{}}};
  const calls=[];let created;
  await context.route('**/api/**', async route => {
   const req=route.request(); const path=new URL(req.url()).pathname; let result=[];
   if(path==='/api/auth/me') result={id:1,username:'admin',name:'Admin',active:true,system_admin:true};
   else if(path==='/api/auth/method') result={mode:'ACCOUNT',type:'PLATFORM_ACCOUNT'};
   else if(path==='/api/databases'&&req.method()==='POST') { created=req.postDataJSON(); result={...db,...created,id:'ob_new',revision:1}; }
   else if(path==='/api/databases') result=[db];
   else if(path==='/api/databases/ob_ui') result=db;
   else if(path==='/api/management/call') {
    const call=req.postDataJSON(); calls.push(call);
    if(call.action==='get_auth_config') result={revision:1,providers:[],policies:[],bindings:[]};
    else if(call.action==='update_database') { db={...db,...call.arguments.database,revision:db.revision+1}; result=db; }
    else if(call.action==='test_database_connection') result={connected:true,empty:true,table_count:0,schemas:['business','sales']};
    else if(call.action==='get_database'||call.action==='get_discovery') result=db;
   }
   await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(result)});
  });
  const page=await context.newPage();page.setDefaultTimeout(15000);
  const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/database?database=ob_ui');
  const schema=page.getByLabel('默认 Schema（可选）',{exact:true});
  await schema.fill('sales');
  await page.getByRole('button',{name:'保存配置',exact:true}).click();
  await page.getByText('Schema 范围已保存',{exact:true}).waitFor();
  const saved=calls.find(c=>c.action==='update_database').arguments.database.connection;
  assert.equal(saved.schema,'sales'); assert.equal(saved.database,'sales');
  await page.getByRole('button',{name:'＋ 数据库',exact:true}).click();
  const modal=page.getByRole('dialog',{name:'添加数据库',exact:true});
  await modal.getByLabel('显示名称',{exact:true}).fill('OceanBase acceptance');
  await modal.getByLabel('数据库类型',{exact:true}).click();
  await page.getByText('OceanBase（MySQL 模式）',{exact:true}).last().click();
  assert.equal(await modal.getByLabel('端口',{exact:true}).inputValue(),'2881');
  assert.equal(await modal.getByLabel('Service name',{exact:true}).count(),0);
  assert.equal(await modal.getByLabel('默认 Schema（可选）',{exact:true}).count(),0);
  await modal.getByText('从连接 URL 填写',{exact:true}).click();
  await modal.getByLabel('连接 URL',{exact:true}).fill('jdbc:oceanbase://fixture:2883/business?user=reader%40tenant%23cluster&password=fixture-password&useSSL=false');
  await modal.getByText('已解析连接 URL，请核对下方字段。',{exact:true}).waitFor();
  assert.equal(await modal.getByLabel('端口',{exact:true}).inputValue(),'2883');
  assert.equal(await modal.getByLabel('数据库只读账号',{exact:true}).inputValue(),'reader@tenant#cluster');
  await modal.getByRole('button',{name:'测试连接',exact:true}).click();
  await modal.getByText('连接成功，发现 0 张表。',{exact:true}).waitFor();
  const tested=calls.filter(c=>c.action==='test_database_connection').at(-1).arguments;
  assert.equal(tested.database.driver,'oceanbase');
  assert.equal(tested.database.connection.schema,'business');
  assert.equal(tested.database.connection.database,'business');
  assert.equal(tested.database.connection.tls_mode,'disable');
  assert.equal(tested.password,'fixture-password');
  if(process.env.DATASCOPE_SCREENSHOT) await page.screenshot({path:process.env.DATASCOPE_SCREENSHOT,animations:'disabled'});
  await modal.getByRole('button',{name:'添加数据库',exact:true}).click();
  await modal.waitFor({state:'hidden'});
  assert.equal(created.driver,'oceanbase');
  assert.equal(created.connection.port,2883);
  assert.equal(created.connection.schema,'business');
  assert.equal(created.password,'fixture-password');
  assert.deepEqual(errors,[]);
  console.log('PASS OceanBase selection, default/custom ports, tenant URL, TLS, connection test, creation payload and Schema/database synchronization');
 } finally { if(browser) await browser.close();server.kill(); }
})().catch(e=>{console.error(e);process.exitCode=1});
