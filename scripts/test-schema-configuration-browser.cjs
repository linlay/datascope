// UI contract acceptance with mocked APIs; never connects to a business database.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { resolve } = require('node:path');
const assert = require('node:assert/strict');
const root = resolve(__dirname, '..');
const base = 'http://127.0.0.1:18196';
const server = spawn(process.execPath, [resolve(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '18196'], {cwd:resolve(root,'web'),stdio:'ignore'});
const pause = ms => new Promise(resolve => setTimeout(resolve,ms));
let browser;
(async()=>{
 try {
  for(let i=0;i<100;i++){try{if((await fetch(base)).ok)break}catch{}await pause(100)}
  browser=await chromium.launch({headless:true,executablePath:process.env.DATASCOPE_CHROMIUM});
  const context=await browser.newContext({viewport:{width:1440,height:1000},locale:'zh-CN'});
  let db={id:'schema_ui',name:'Schema UI',driver:'oracle',path:'',schema_identity_version:1,connection:{host:'fixture',port:1521,service_name:'FREEPDB1',schema:'',allowed_schemas:['A','B'],username:'reader'},revision:1,mcp_enabled:false,tools:['list_tables','describe_table','query'],config:{version:1,schemes:[{scheme:'NONE',name:'Public'}],tables:[]},discovery:{version:1,tables:[],annotations:{},reports:[]}};
  const candidates=[{name:'A.RECORDS',schema:'A'},{name:'B.RECORDS',schema:'B'},...Array.from({length:21},(_,i)=>({name:`A.TABLE_${i}`,schema:'A'}))];
  const calls=[];
  await context.route('**/api/**',async route=>{
   const request=route.request();const path=new URL(request.url()).pathname;let result={};
   if(path==='/api/auth/me')result={id:1,username:'admin',name:'Admin',active:true,system_admin:true};
   else if(path==='/api/auth/method')result={mode:'ACCOUNT',type:'PLATFORM_ACCOUNT'};
   else if(path==='/api/databases')result=[db];
   else if(path==='/api/databases/schema_ui')result=db;
   else if(path==='/api/management/call'){
    const call=request.postDataJSON();calls.push(call);const args=call.arguments;
    if(call.action==='get_auth_config')result={revision:1,providers:[],policies:[],bindings:[]};
    else if(call.action==='test_database_connection')result={connected:true,empty:true,table_count:0,schemas:['A','B']};
    else if(call.action==='list_schemas')result=['A','B'];
    else if(call.action==='list_table_candidates'){const matches=candidates.filter(t=>t.name.toLowerCase().includes((args.query||'').toLowerCase()));result={items:matches.slice(args.offset,args.offset+args.limit),total:matches.length};}
    else if(call.action==='enroll_tables'){
     assert.deepEqual(args.table_names,['A.RECORDS']);assert.equal(args.revision,2);
     db={...db,revision:3,config:{...db.config,tables:[{table:'A.RECORDS',enabled:false,passthrough:true,scheme:'NONE',columns:['ID']}]},discovery:{...db.discovery,tables:[{name:'A.RECORDS',schema:'A',table_name:'RECORDS',comment:'A records',columns:[{name:'ID',type:'NUMBER',comment:'Identifier'}],indexes:[]}]}};result=db;
    } else if(call.action==='update_database'){assert.equal(args.database.connection.schema,'');assert.deepEqual(args.database.connection.allowed_schemas,['A','B']);db={...db,revision:db.revision+1,connection:args.database.connection};result=db;}
    else if(call.action==='get_database'||call.action==='get_discovery')result=db;
    else result=[];
   }
   await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(result)});
  });
  const page=await context.newPage();page.setDefaultTimeout(12000);const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/database?database=schema_ui');
  await page.getByText('Schema 范围',{exact:true}).waitFor().catch(async error=>{console.error('UI diagnostic',await page.locator('body').innerText(),errors);throw error});
  await page.getByRole('button',{name:'保存配置',exact:true}).click();
  await page.waitForFunction(()=>document.body.innerText.includes('Schema 范围已保存'));
  assert(calls.some(c=>c.action==='update_database'));
  await page.getByRole('button',{name:'增加数据库',exact:true}).click();
  const modal=page.getByRole('dialog',{name:'增加数据库',exact:true});
  await modal.getByLabel('显示名称',{exact:true}).fill('Oracle no default');
  await modal.getByLabel('数据库类型',{exact:true}).click();await page.getByText('Oracle',{exact:true}).last().click();
  assert.equal(await modal.getByLabel('端口',{exact:true}).inputValue(),'1521');
  await modal.getByLabel('数据库地址',{exact:true}).fill('fixture');
  await modal.getByLabel('数据库只读账号',{exact:true}).fill('reader');
  await modal.getByLabel('数据库密码',{exact:true}).fill('fixture-password');
  assert.equal(await modal.getByLabel('默认 Schema（可选）',{exact:true}).inputValue(),'');
  const before=calls.filter(c=>c.action==='test_database_connection').length;
  await modal.getByRole('button',{name:'测试连接',exact:true}).click();await pause(200);
  assert.equal(calls.filter(c=>c.action==='test_database_connection').length,before,'service_name must be required');
  await modal.getByLabel('Service name',{exact:true}).fill('FREEPDB1');
  await modal.getByRole('button',{name:'测试连接',exact:true}).click();
  await modal.getByText('连接成功，发现 0 张表。',{exact:true}).waitFor().catch(async error=>{console.error(await modal.innerText(),calls.map(c=>c.action));throw error});
  const probe=calls.filter(c=>c.action==='test_database_connection').at(-1);assert(!probe.arguments.database.connection.schema);
  await modal.getByRole('button',{name:/取\s*消/}).click();
  await page.goto(base+'/#/tables?database=schema_ui');
  await page.getByRole('button',{name:'选择登记表',exact:true}).click();
  const tables=page.getByRole('dialog',{name:'选择登记表',exact:true});
  await tables.getByText('A.RECORDS',{exact:true}).waitFor();
  await Promise.all([page.waitForResponse(r=>r.url().includes('/api/management/call')&&r.request().postDataJSON()?.arguments?.offset===20),tables.locator('.ant-pagination-next').click()]);
  assert(calls.some(c=>c.action==='list_table_candidates'&&c.arguments.offset===20));
  await tables.getByLabel('搜索候选表',{exact:true}).fill('records');
  await tables.getByText('A.RECORDS',{exact:true}).waitFor();await tables.getByText('B.RECORDS',{exact:true}).waitFor();
  await tables.getByRole('row').filter({hasText:'A.RECORDS'}).getByRole('checkbox').check();
  await tables.getByRole('button',{name:'登记所选表',exact:true}).click();
  await tables.waitFor({state:'hidden'});
  assert.equal(await page.getByRole('switch',{name:'开放表 A.RECORDS',exact:true}).getAttribute('aria-checked'),'false');
  assert(!await page.locator('.discovery-table-card').getByText('B.RECORDS',{exact:true}).count());
  assert.deepEqual(errors,[]);
  console.log('PASS optional Oracle Schema, required Service name, zero-table connection, schema scope save, candidate search/pagination, explicit enrollment and default-denied publication');
 } finally {if(browser)await browser.close();server.kill();}
})().catch(e=>{console.error(e);process.exitCode=1});
