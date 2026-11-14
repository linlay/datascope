// Mocked UI acceptance: no business databases are accessed.
const { chromium } = require(process.env.DATASCOPE_PLAYWRIGHT || 'playwright');
const { spawn } = require('node:child_process');
const { resolve } = require('node:path');
const assert = require('node:assert/strict');
const root = resolve(__dirname, '..');
const port = 20000 + Math.floor(Math.random() * 20000);
const base = `http://127.0.0.1:${port}`;
const viteEntry = `import { createServer } from ${JSON.stringify(resolve(root, 'web/node_modules/vite/dist/node/index.js'))};
const server = await createServer({server:{host:'127.0.0.1',port:${port},strictPort:true,hmr:false,watch:null}}); await server.listen();`;
const server = spawn(process.execPath, ['--input-type=module', '--eval', viteEntry], { cwd: resolve(root, 'web'), stdio: 'ignore' });
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));
let browser;
(async () => {
 try {
  for (let i = 0; i < 100; i++) { try { if ((await fetch(base)).ok) break; } catch {} await pause(100); }
  browser = await chromium.launch({ headless: true, executablePath: process.env.DATASCOPE_CHROMIUM });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
  const source = { id: 'source', name: 'Source', driver: 'postgresql', path: '', revision: 1, mcp_enabled: false, tools: [], connection: { host: 'source-host', port: 5432, database: 'source', schema: 'public', allowed_schemas: ['public'], username: 'reader', credential_id: 'secret-id', password_env: 'SECRET', tls_mode: 'verify-full' }, config: { version: 1, schemes: [], tables: [{ table: 'public.records', columns: ['id'] }] } };
  const items = [source]; const creates = []; const calls = []; let commitCount = 0;
  await context.route('**/api/**', async route => {
   const request = route.request(); const path = new URL(request.url()).pathname; let result = []; let status = 200;
   if (path === '/api/auth/me') result = { id: 1, username: 'admin', active: true, system_admin: true };
   else if (path === '/api/auth/method') result = { mode: 'ACCOUNT', type: 'PLATFORM_ACCOUNT' };
   else if (path === '/api/databases' && request.method() === 'POST') {
    const body = request.postDataJSON(); creates.push(body);
    result = { ...source, ...body, id: 'new_' + creates.length, config: { version: 1, schemes: [], tables: [] } }; items.push(result);
   } else if (path === '/api/databases') result = items;
   else if (path.startsWith('/api/databases/')) result = items.find(db => db.id === path.split('/').at(-1));
   else if (path === '/api/management/call') {
    const call = request.postDataJSON(); calls.push(call); const args = call.arguments;
    if (call.action === 'get_auth_config') result = { revision: 1, providers: [], policies: [], bindings: [] };
    else if (call.action === 'get_database' || call.action === 'get_discovery') result = items.find(db => db.id === args.database_id);
    else if (call.action === 'enroll_tables') {
     assert.equal(args.database_id, 'new_2'); assert.deepEqual(args.table_names, ['public.records']);
     const target = items.find(db => db.id === args.database_id); target.config.tables = source.config.tables; target.revision++; result = target;
    } else if (call.action === 'preview_clone_configuration') {
     assert.equal(args.database_id, 'new_2'); assert.equal(args.clone.source_database_id, 'source');
     result = { source_database_id: 'source', target_database_id: 'new_2', source_revision: 1, target_revision: 2, source_token: 'src', target_token: 'dst', confirmation: 'preview', scope: args.clone.scope, items: [], issues: [], warnings: [] };
    } else if (call.action === 'clone_configuration') {
     assert.equal(args.database_id, 'new_2'); assert.equal(args.clone.confirmation, 'preview'); assert.equal(args.clone.confirmed, true);
     assert.equal(args.clone.scope.authentication, false); commitCount++;
     if (commitCount === 1) { status = 409; result = { error: 'Configuration changed. Retry preview.' }; }
     else result = { applied: true };
    }
   }
   await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(result) });
  });
  const page = await context.newPage(); page.setDefaultTimeout(12000); const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const select = async (dialog, label, option) => { await dialog.getByLabel(label, { exact: true }).click(); await page.locator('.ant-select-dropdown:visible').getByText(option, { exact: true }).click(); };
  await page.goto(base + '/#/database?database=source');
  await page.getByRole('button', { name: '增加数据库', exact: true }).click();
  let dialog = page.getByRole('dialog', { name: '增加数据库', exact: true });
  await dialog.getByLabel('显示名称', { exact: true }).fill('Fresh');
  await dialog.getByLabel('SQLite 文件路径（服务端）', { exact: true }).fill('/new.sqlite3');
  await dialog.getByRole('button', { name: '增加数据库', exact: true }).click();
  await dialog.waitFor({ state: 'hidden' }); await page.waitForURL(/database=new_1/); assert.equal(creates.length, 1); assert.equal(creates[0].driver, 'sqlite');
  assert(!calls.some(c => c.action.includes('clone_configuration')));
  await page.getByRole('button', { name: '增加数据库', exact: true }).click();
  dialog = page.getByRole('dialog', { name: '增加数据库', exact: true });
  await select(dialog, '增加方式', '从已有数据库克隆');
  await select(dialog, '来源库', 'Source (source)');
  for (let i = 0; i < 50 && await dialog.getByLabel('数据库只读账号', { exact: true }).inputValue() !== 'reader'; i++) await pause(100);
  assert.equal(await dialog.getByLabel('数据库地址', { exact: true }).inputValue(), 'source-host');
  assert.equal(await dialog.getByLabel('数据库密码', { exact: true }).inputValue(), '');
  assert.equal(await dialog.getByLabel('数据库只读账号', { exact: true }).inputValue(), 'reader');
  await dialog.getByLabel('显示名称', { exact: true }).fill('Clone');
  await dialog.getByLabel('数据库地址', { exact: true }).fill('target-host');
  await dialog.getByLabel('数据库名称', { exact: true }).fill('target');
  await dialog.getByLabel('数据库密码', { exact: true }).fill('new-password');
  await dialog.getByRole('button', { name: '创建新库并预览克隆', exact: true }).click();
  const confirm = dialog.getByRole('checkbox', { name: '我已核对新数据库与所选配置，确认克隆', exact: true });
  await confirm.check().catch(async e => { console.error(await dialog.innerText(), creates, calls.slice(-6), errors); throw e; });
  await dialog.getByRole('button', { name: '确认克隆到新数据库', exact: true }).click();
  await dialog.getByText('Configuration changed. Retry preview.', { exact: true }).waitFor();
  await dialog.getByRole('button', { name: '重新检查', exact: true }).click();
  await confirm.check();
  await dialog.getByRole('button', { name: '确认克隆到新数据库', exact: true }).click();
  await dialog.waitFor({ state: 'hidden' });
  assert.equal(creates.length, 2); assert.equal(commitCount, 2);
  assert.equal(creates[1].connection.host, 'target-host'); assert.equal(creates[1].password, 'new-password');
  assert(!creates[1].connection.credential_id); assert(!creates[1].connection.password_env);
  assert.equal(source.connection.host, 'source-host');
  assert.deepEqual(errors, []);
  console.log('PASS fresh database, clone connection prefill, credential isolation, target enrollment, preview confirmation and retry without duplicate creation');
 } finally { if (browser) await browser.close(); server.kill(); }
})().catch(e => { console.error(e); process.exitCode = 1; });
