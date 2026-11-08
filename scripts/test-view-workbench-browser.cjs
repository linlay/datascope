// Run through TestWorkbenchBrowserHarness; uses only its isolated databases.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { resolve } = require('node:path');
const { mkdir } = require('node:fs/promises');
const assert = require('node:assert/strict');

const root = resolve(__dirname, '..');
const base = 'http://127.0.0.1:18092';
const output = process.env.DATASCOPE_VIEW_SCREENSHOTS;
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
const server = spawn(process.execPath, [resolve(root, 'web/node_modules/vite/bin/vite.js'), '--host', '127.0.0.1', '--port', '18092'], {
 cwd: resolve(root, 'web'), env: { ...process.env, WEB_PORT: '18092', ADMIN_API_URL: 'http://127.0.0.1:18091' }, stdio: 'ignore',
});
const html = title => `<!doctype html><h1>${title}</h1><pre id="data"></pre><button id="call">List actions</button><pre id="response"></pre><script>
let channel;addEventListener('message',event=>{const m=event.data;if(m.type==='datascope:view:init'){channel=m.channel;document.getElementById('data').textContent=JSON.stringify(m.data);}if(m.type==='datascope:view:response')document.getElementById('response').textContent=JSON.stringify(m.error||m.result);});
document.getElementById('call').onclick=()=>parent.postMessage({type:'datascope:view:request',channel,id:1,method:'tools/call',params:{name:'list_actions',arguments:{}}},'*');parent.postMessage({type:'datascope:view:ready'},'*');</script>`;

(async () => {
 let browser, page;
 try {
  for (let i = 0; i < 100; i++) { try { if ((await fetch(base)).ok) break; } catch {} await pause(100); }
  if (output) await mkdir(output, { recursive: true });
  browser = await chromium.launch({ headless: true, executablePath: process.env.DATASCOPE_CHROMIUM });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const login = await context.request.post(base + '/api/auth/login', { data: { username: 'admin', password: 'admin-test-password' } });
  assert.equal(login.status(), 200);
  const call = async (action, args) => {
   const response = await context.request.post(base + '/api/management/call', { data: { action, arguments: { database_id: 'default', ...args } } });
   const body = await response.json(); assert.equal(response.status(), 200, JSON.stringify(body)); return body;
  };
  let db = await (await context.request.get(base + '/api/databases/default')).json();
  await call('configure_views', { database: { ...db, discovery: undefined, views: [
   { id: 'first', name: 'First View', type: 'view', description: 'Published preview', html: html('First HTML'), published: true },
   { id: 'draft', name: 'Draft View', type: 'view', description: 'Unpublished preview', html: html('Draft HTML'), published: false },
   { id: 'second', name: 'Second View', type: 'view', description: 'Second published view', html: html('Second HTML'), published: true },
  ] } });
  db = await (await context.request.get(base + '/api/databases/default')).json();
  await call('save_database', { database: { ...db, discovery: undefined, authentication: { type: 'local', source: 'GLOBAL', access: { id: 'workbench', name: 'Workbench test', mode: 'ACCOUNT', provider_id: 'business-account', permissions: ['query', 'knowledge.read', 'action'], business_user_id: 10 } } } });
  page = await context.newPage(); page.setDefaultTimeout(15000);
  const errors = [], operations = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => { if (request.url().includes('/business-mcp') && request.method() === 'POST') operations.push(request.postDataJSON()); });
  const url = (id = 'first', mode = '') => `${base}/#/view-workbench/${id}?database=default${mode ? '&mode=' + mode : ''}`;
  const frame = () => page.frameLocator('.view-workbench iframe');
  const inputData = value => page.locator('.view-workbench .cm-content[contenteditable="true"]').fill(value);
  const select = async name => {
   await page.getByRole('combobox', { name: '选择视图', exact: true }).click();
   await page.locator('.ant-select-dropdown:visible .ant-select-item-option').filter({ hasText: name }).click();
  };
  await page.goto(`${base}/#/database?database=default`);
  await page.getByRole('menuitem', { name: /视图工作台/ }).click();
  await page.waitForURL(url());
  await frame().getByRole('heading', { name: 'First HTML', exact: true }).waitFor();
  await page.locator('.view-workbench .action-list-item').filter({ hasText: 'Draft View' }).click();
  await page.waitForURL(url('draft'));
  await frame().getByRole('heading', { name: 'Draft HTML', exact: true }).waitFor();
  await page.goBack(); await page.waitForURL(url());
  await frame().getByRole('heading', { name: 'First HTML', exact: true }).waitFor();
  await page.getByRole('tab', { name: 'HTML', exact: true }).click();
  assert.equal(new URLSearchParams(page.url().split('?')[1]).get('detailView'), 'html');
  await page.reload(); await page.getByRole('tab', { name: 'HTML', exact: true }).waitFor();
  assert.equal(await page.getByRole('tab', { name: 'HTML', exact: true }).getAttribute('aria-selected'), 'true');
  await page.getByRole('tab', { name: '预览', exact: true }).click();
  await inputData(JSON.stringify({ input: { label: 'Mock label' }, result: { marker: 'Mock result' } }));
  await page.getByRole('button', { name: '渲染预览', exact: true }).click();
  await frame().locator('#data').filter({ hasText: 'Mock result' }).waitFor();
  await frame().getByRole('button', { name: 'List actions', exact: true }).click();
  await frame().locator('#response').filter({ hasText: '预览未连接业务身份' }).waitFor();
  assert.equal(operations.length, 0, 'mock preview made a business MCP request');
  await inputData('{"input":[] }');
  await page.getByRole('button', { name: '渲染预览', exact: true }).click();
  await page.getByText('视图数据必须是 JSON 对象，input 也必须是对象。', { exact: true }).waitFor();
  assert.match(await frame().locator('#data').innerText(), /Mock result/);
  console.log('PASS navigation, drafts, mock input/result, source tab, history and no business calls in mock preview');
  await page.getByRole('button', { name: '视图配置', exact: true }).click();
  await page.waitForURL(`${base}/#/view-configuration/first?database=default`);
  await page.getByRole('button', { name: '视图工作台', exact: true }).click();
  await page.waitForURL(url());
  await page.getByRole('tab', { name: '真实业务预览', exact: true }).click();
  await page.waitForURL(url('first', 'business'));
  await page.getByLabel('业务用户名', { exact: true }).fill('E1');
  await page.getByLabel('业务账号密码', { exact: true }).fill('business-password');
  await page.getByRole('button', { name: '获取业务 Token', exact: true }).click();
  await page.getByRole('button', { name: '获取视图列表', exact: true }).click();
  await page.getByRole('combobox', { name: '选择视图', exact: true }).click();
  assert.equal(await page.locator('.ant-select-dropdown:visible .ant-select-item-option').count(), 2);
  assert.equal(await page.locator('.ant-select-dropdown:visible').getByText('Draft View', { exact: true }).count(), 0);
  await page.keyboard.press('Escape');
  assert.equal(operations.filter(v => v.operation === 'resources/read').length, 0, 'listing views rendered a live frame');
  await page.getByRole('button', { name: '载入视图', exact: true }).click();
  await frame().getByRole('heading', { name: 'First HTML', exact: true }).waitFor();
  await inputData('{"label":"Business label"}');
  await page.getByRole('button', { name: '载入视图', exact: true }).click();
  await frame().locator('#data').filter({ hasText: 'Business label' }).waitFor();
  await frame().getByRole('button', { name: 'List actions', exact: true }).click();
  await frame().locator('#response').filter({ hasText: 'rename_item' }).waitFor();
  await select('Second View'); await page.waitForURL(url('second', 'business'));
  assert.equal(await page.locator('.view-workbench iframe').count(), 0);
  await page.getByRole('button', { name: '载入视图', exact: true }).click();
  await frame().getByRole('heading', { name: 'Second HTML', exact: true }).waitFor();
  await page.goBack(); await page.waitForURL(url('first', 'business'));
  assert.equal(await page.locator('.view-workbench iframe').count(), 0);
  await page.getByRole('button', { name: '载入视图', exact: true }).click();
  await frame().getByRole('heading', { name: 'First HTML', exact: true }).waitFor();
  if (output) await page.screenshot({ path: resolve(output, 'view-workbench-business.png'), fullPage: true });
  await page.getByRole('button', { name: '清除业务 Token', exact: true }).click();
  await page.locator('.view-workbench iframe').waitFor({ state: 'detached' });
  await page.reload(); await page.getByRole('tab', { name: '真实业务预览', exact: true }).waitFor();
  assert.equal(await page.getByRole('tab', { name: '真实业务预览', exact: true }).getAttribute('aria-selected'), 'true');
  assert.equal(await page.getByLabel('本人业务 Token', { exact: true }).inputValue(), '');
  console.log('PASS configuration links, real MCP authentication/list/read, bridge calls, selected-view URL and identity reset');
  await page.getByRole('tab', { name: '模拟数据预览', exact: true }).click();
  await frame().getByRole('heading', { name: 'First HTML', exact: true }).waitFor();
  if (output) await page.screenshot({ path: resolve(output, 'view-workbench.png'), fullPage: true });
  await page.setViewportSize({ width: 700, height: 950 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  if (output) await page.screenshot({ path: resolve(output, 'view-workbench-narrow.png'), fullPage: true });
  const reader = await browser.newContext();
  assert.equal((await reader.request.post(base + '/api/auth/login', { data: { username: 'reader', password: 'reader-test-password' } })).status(), 200);
  const readerPage = await reader.newPage();
  await readerPage.goto(url());
  await readerPage.getByRole('menuitem', { name: /知识工作台/ }).waitFor();
  assert.equal(await readerPage.getByRole('menuitem', { name: /视图工作台/ }).count(), 0);
  assert.equal(await readerPage.locator('.view-workbench').count(), 0);
  assert.deepEqual(errors, []);
  console.log('PASS narrow layout, menu permissions, unauthorized deep link and no JavaScript errors');
 } catch (error) { if (page && output) await page.screenshot({ path: resolve(output, 'failure.png'), fullPage: true }); throw error; }
 finally { if (browser) await browser.close(); server.kill('SIGTERM'); }
})().catch(error => { console.error(error); process.exitCode = 1; });
