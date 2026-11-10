// Invoked by TestAssistantBrowser against its isolated SQLite fixture and model.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const assert = require('node:assert/strict');

(async () => {
 const browser = await chromium.launch({ executablePath: process.env.DATASCOPE_CHROMIUM });
 const page = await browser.newPage();
 try {
  page.setDefaultTimeout(10000);
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const base = process.argv[2];
  // Ant Design keeps its accessible loading icon during the exit animation.
  const button = name => {
   const control=page.getByRole('button',{name,exact:true});
   return typeof name==='string'?control.or(page.getByRole('button',{name:'loading '+name,exact:true})):control;
  };
  const label = name => page.getByLabel(name,{exact:true});
  await page.goto(base+'/#/action-configuration/rename_item?database=default');
  await label('用户名').fill('admin'); await label('密码').fill('admin-test-password');
  await button(/登\s*录/).click();
  await button('新增动作').click();
  await label('名称').fill('assistant_browser');await label('标题').fill('Rename owned');
  await label('说明').fill('Update only the current manager’s item');
  await button('AI 辅助').click();await label('动作需求').fill('Rename items.name by id for owner_id = ctx.userId');
  await button('生成建议').click();
  await button('应用到草稿').waitFor();
  assert.match(await page.locator('.assistant-comparison').last().innerText(),/owner_id/);
  await label('采纳脚本').uncheck();await button('应用到草稿').click();
  assert.match(await page.locator('.cm-content').first().innerText(),/records/);
  await button('生成建议').click();await label('采纳脚本').check();await button('应用到草稿').click();
  assert.match(await page.locator('.cm-content').first().innerText(),/owner_id/);
  await button('保存').click();await page.getByText('业务操作配置已保存',{exact:true}).waitFor();

  await button('AI 辅助').click();await label('动作需求').fill('Update the current draft');
  // Simulate a provider that completes even after the browser abandons the request.
  await page.evaluate(()=>{
   const fetchOriginal=window.fetch.bind(window);
   window.fetch=(url,init)=>init?.body?.includes('"assist_action"')?fetchOriginal(url,{...init,signal:undefined}):fetchOriginal(url,init);
  });
  let release,started;
  const pending=new Promise(resolve=>{started=resolve});
  const gate=new Promise(resolve=>{release=resolve});
  const endpoint='**/api/management/call';
  await page.route(endpoint,async route=>{
   started();await gate;await route.fulfill({json:{script:'old proposal',parameters:{}}});
  },{times:1});
  await button('生成建议').click();await pending;
  await label('标题').fill('Newer manual title');
  const completed=page.waitForResponse(r=>r.url().endsWith('/api/management/call'));
  release();await (await completed).finished();
  await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
  assert.equal(await button('应用到草稿').count(),0);
  assert.equal(await label('标题').inputValue(),'Newer manual title');
  await page.route(endpoint,route=>route.fulfill({status:503,json:{error:'offline model'}}),{times:1});
  await button('生成建议').click();await page.getByText('offline model',{exact:true}).waitFor();
  await label('说明').fill('Manually edited while offline');await button('保存').click();
  await page.waitForFunction(()=>document.querySelector('.action-editor-panel .ant-tag-orange')===null);

  await page.goto(base+'/#/action-workbench/assistant_browser?database=default&identity=10');
  await button('AI 生成测试参数').click();await button('采用测试用例').click();
  await page.getByRole('textbox',{name:'预期结果'}).fill('One affected row for the owner, denial for another user');
  await button('试运行（回滚）').click();
  await page.getByText('预览完成，事务已回滚',{exact:true}).waitFor();
  await page.getByText('影响 1 行',{exact:true}).waitFor();
  await button('AI 解释试运行').click();await page.getByText(/Trial diagnosis:.*affected_rows/).waitFor();
  await label('业务身份编号').fill('20');await label('业务身份编号').press('Tab');
  await button('试运行（回滚）').click();await page.getByText('invisible',{exact:true}).first().waitFor();
  await button('AI 解释试运行').click();await page.getByText(/Trial diagnosis:.*business_rule_denied/).waitFor();
  assert.equal(await button('带回动作草稿').count(),0);
  await page.goto(base+'/#/action-workbench/needs_repair?database=default&identity=10');
  await button('AI 生成测试参数').click();await button('采用测试用例').click();
  await button('试运行（回滚）').click();await button('AI 解释试运行').click();
  await button('带回动作草稿').click();
  assert.match(await label('动作需求').inputValue(),/undefined helper/);
  assert.match(await page.locator('.cm-content').first().innerText(),/missingHelper/);
  assert.deepEqual(errors,[]);
 } catch(error){
  console.error(await page.getByRole('main').ariaSnapshot());throw error;
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1});
