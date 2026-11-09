// UI acceptance uses mocked APIs and never writes a business database.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { resolve } = require('node:path');
const assert = require('node:assert/strict');
const root = resolve(__dirname, '..');
const base = 'http://127.0.0.1:18206';
const server = spawn(process.execPath, [resolve(root,'web/node_modules/vite/bin/vite.js'),'--host','127.0.0.1','--port','18206'], {cwd:resolve(root,'web'),stdio:'ignore'});
const pause = ms => new Promise(resolve=>setTimeout(resolve,ms));
let browser;
(async()=>{
 try {
  for(let i=0;i<100;i++){try{if((await fetch(base)).ok)break}catch{}await pause(100)}
  browser=await chromium.launch({headless:true,executablePath:process.env.DATASCOPE_CHROMIUM});
  const context=await browser.newContext({viewport:{width:1440,height:1000},locale:'zh-CN'});
  let db={id:'ddl_ui',name:'DDL UI',driver:'sqlite',path:'/fixture/business.sqlite',revision:1,ddl_enabled:false,mcp_enabled:false,tools:[],config:{version:1,schemes:[],tables:[]}};
  const network={...db,id:'network',name:'Network',driver:'mysql',connection:{host:'fixture',port:3306,username:'reader'}};
  const columns=[{name:'id',type:'INTEGER',primary_key:1,not_null:false,hidden:0,sql:'id INTEGER PRIMARY KEY'},{name:'name',type:'TEXT',primary_key:0,not_null:false,hidden:0,sql:'name TEXT'}];
  let objects=[];const calls=[];let plan;let execution;
  await context.route('**/api/**',async route=>{
   const request=route.request();const path=new URL(request.url()).pathname;let result={};
   if(path==='/api/auth/me')result={id:1,username:'admin',name:'Admin',active:true,system_admin:true};
   else if(path==='/api/auth/method')result={mode:'ACCOUNT',type:'PLATFORM_ACCOUNT'};
   else if(path==='/api/databases')result=[db,network];
   else if(path==='/api/management/call'){
    const call=request.postDataJSON();calls.push(call);const args=call.arguments;
    if(call.action==='update_database'){assert.equal(args.database.revision,db.revision);db={...db,...args.database,revision:db.revision+1};result=db;}
    else if(call.action==='list_ddl_objects'){const filtered=objects.filter(o=>(!args.object_type||o.object_type===args.object_type)&&o.name.includes(args.query||''));result={items:filtered,total:filtered.length};}
    else if(call.action==='get_ddl_object')result=objects.find(o=>o.name===args.name);
    else if(call.action==='preview_ddl'){
     assert.equal(args.changes[0].name,'clients');assert.equal(args.changes[0].definition.columns[0].primary_key,1);
     plan={plan_id:'p_11111111111111111111111111111111',sql:['CREATE TABLE clients(id INTEGER PRIMARY KEY,name TEXT)'],diff:[{object_type:'table',name:'clients',before:'',after:'CREATE TABLE clients(id INTEGER PRIMARY KEY,name TEXT)'}],warnings:[],requires_rebuild:false};result=plan;
    }
    else if(call.action==='apply_ddl'){
     assert.equal(args.plan_id,plan.plan_id);assert(args.request_id);
     objects=[{object_type:'table',name:'clients',parent_name:'clients',sql:plan.sql[0],definition:{columns},dependencies:[]}];
     execution={execution_id:'e_11111111111111111111111111111111',plan_id:plan.plan_id,actor_id:1,status:'succeeded',started_at:new Date().toISOString(),backup_id:'e_11111111111111111111111111111111',discovery_status:'review_required',sql:plan.sql,diff:plan.diff};result=execution;
    }
    else if(call.action==='list_ddl_executions')result={items:execution?[execution]:[],total:execution?1:0};
    else if(call.action==='get_auth_config')result={revision:1,providers:[],policies:[],bindings:[]};
    else if(call.action==='get_database'||call.action==='get_discovery')result=db;
    else result=[];
   }else if(path==='/api/databases/ddl_ui')result=db;
   await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(result)});
  });
  const page=await context.newPage();page.setDefaultTimeout(12000);const errors=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto(base+'/#/database-configuration?database=ddl_ui');
  const ddlMenu=page.getByRole('menuitem',{name:/DDL 管理/});
  await page.getByRole('switch',{name:'打开 DDL 管理',exact:true}).waitFor();assert.equal(await ddlMenu.count(),0);
  await page.getByRole('switch',{name:'打开 DDL 管理',exact:true}).click();await ddlMenu.waitFor().catch(async e=>{console.error('DDL UI diagnostic',await page.locator('body').innerText(),calls,errors);throw e});await ddlMenu.click();
  await page.getByRole('button',{name:/新建/}).click();
  const create=page.getByRole('dialog',{name:'新建 表',exact:true});await create.getByLabel('名称',{exact:true}).fill('clients');
  await create.getByRole('button',{name:'预览变更',exact:true}).click();
  const preview=page.getByRole('dialog',{name:'预览 DDL 变更',exact:true});await preview.waitFor().catch(async e=>{console.error('preview diagnostic',await page.locator('body').innerText(),JSON.stringify(calls),errors);throw e});
  assert(!calls.some(c=>c.action==='apply_ddl'),'preview must not apply');
  await preview.getByRole('button',{name:'执行变更',exact:true}).click();
  const details=page.getByRole('dialog',{name:'DDL 执行详情',exact:true});await details.waitFor();await details.getByText('备份编号',{exact:false}).waitFor();await details.getByRole('button',{name:/关\s*闭/,exact:true}).click();
  await page.getByRole('cell',{name:'clients',exact:true}).click();await page.getByRole('button',{name:'新增字段',exact:true}).waitFor();
  if(process.env.DATASCOPE_DDL_SCREENSHOT)await page.screenshot({path:process.env.DATASCOPE_DDL_SCREENSHOT,fullPage:true});
  await page.getByRole('button',{name:'新增字段',exact:true}).click();const field=page.getByRole('dialog',{name:'新建 字段',exact:true});await field.locator('.cm-content').waitFor();await field.getByRole('button',{name:/取\s*消/}).click();
  await page.getByRole('tab',{name:'执行记录',exact:true}).click();await page.getByRole('button',{name:/详\s*情/,exact:true}).waitFor();
  await page.getByRole('menuitem',{name:/数据库配置/}).click();await page.getByRole('switch',{name:'打开 DDL 管理',exact:true}).click();await ddlMenu.waitFor({state:'hidden'});
  await page.goto(base+'/#/database-configuration?database=network');await page.getByRole('button',{name:'测试连接',exact:true}).waitFor();assert.equal(await page.getByRole('switch',{name:'打开 DDL 管理',exact:true}).count(),0);assert.equal(await ddlMenu.count(),0);
  await page.goto(base+'/#/ddl-management?database=ddl_ui');await page.getByRole('switch',{name:'打开 DDL 管理',exact:true}).waitFor();assert(!page.url().includes('ddl-management'),'disabled bookmark must redirect');
  assert.deepEqual(errors,[]);
  console.log('PASS SQLite-only DDL switch, conditional menu, table form, preview/apply, backup receipt, field editor, history and disabled-route handling');
 }finally{if(browser)await browser.close();server.kill()}
})().catch(e=>{console.error(e);process.exitCode=1});
