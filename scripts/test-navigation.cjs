// Exercise the actual TypeScript router without a browser or application server.
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const vm = require('node:vm');
const ts = require('../web/node_modules/typescript');
const source = ts.transpileModule(readFileSync(resolve(__dirname, '../web/src/navigation.ts'), 'utf8'), {
 compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
}).outputText;
const location = { hash: '#/sql?database=default&example=3&identity=175&limit=50' };
const writes = [];
const sandbox = {
 exports: {}, URLSearchParams,
 require: name => {
  if (name === 'react') return { createContext: () => ({}) };
  if (name === 'antd') return {};
  throw new Error(`Unexpected import ${name}`);
 },
 window: { location, history: {
  state: null,
  replaceState(state, _, hash) { this.state = state; location.hash = hash; writes.push(hash); },
  pushState(state, _, hash) { this.state = state; location.hash = hash; writes.push(hash); },
 } },
};
vm.runInNewContext(source, sandbox);
const { readRoute, setRouteParams, navigate, selectResource } = sandbox.exports;
assert.equal(readRoute().page, 'sql-workbench');
// App initialization writes the database parameter and canonicalizes old links.
setRouteParams({ database: 'default' }, true);
assert.equal(location.hash, '#/query-workbench?database=default&example=3&identity=175&limit=50');
assert.equal(readRoute().params.get('example'), '3');
navigate('sql-workbench', undefined, { database: 'other' });
assert.equal(location.hash, '#/query-workbench?database=other');
assert.equal(readRoute('#/sql-workbench?database=default').page, 'sql-workbench');
for (const bookmark of ['sql', 'sql-workbench', 'query-workbench']) {
 location.hash = `#/${bookmark}/example?database=default&example=3&identity=175&limit=50`;
 assert.equal(readRoute().page, 'sql-workbench');
 setRouteParams({ database: 'default' }, true);
 assert.equal(location.hash, '#/query-workbench/example?database=default&example=3&identity=175&limit=50');
}
assert.equal(readRoute('#/knowledge-workbench?database=default').page, 'knowledge-workbench');
const configurationPaths = {
 database: 'database-configuration', tables: 'query-configuration',
 actions: 'action-configuration', views: 'view-configuration',
 knowledge: 'knowledge-configuration', mcp: 'mcp-configuration',
};
const selection = '客户/详情';
const encoded = encodeURIComponent(selection);
for (const [page, path] of Object.entries(configurationPaths)) {
 const query = 'database=default&tab=details&mode=identity&identity=175';
 for (const bookmark of [page, path]) {
  location.hash = `#/${bookmark}/${encoded}?${query}`;
  assert.equal(readRoute().page, page);
  assert.equal(readRoute().selection, selection);
  setRouteParams({ database: 'default' }, true);
  assert.equal(location.hash, `#/${path}/${encoded}?${query}`);
 }
 navigate(page, selection, { database: 'other' });
 assert.equal(location.hash, `#/${path}/${encoded}?database=other`);
 selectResource('next');
 assert.equal(location.hash, `#/${path}/next?database=other`);
}
for (const page of ['knowledge-workbench', 'action-workbench', 'view-workbench', 'table', 'audit']) {
 navigate(page, undefined, { database: 'default' });
 assert.equal(location.hash, `#/${page}?database=default`);
 assert.equal(readRoute().page, page);
}
for (const [alias, page] of Object.entries({ discovery: 'tables', policies: 'tables', access: 'database', data: 'table' })) {
 assert.equal(readRoute(`#/${alias}`).page, page);
}
for (const path of ['database', 'database-configuration']) {
 location.hash = `#/${path}?database=default&tab=audit&outcome=success`;
 setRouteParams({ database: 'default' }, true);
 assert.equal(location.hash, '#/audit?database=default&outcome=success');
}
assert.equal(readRoute('#/view-configuration/%invalid').selection, undefined);
assert.equal(readRoute('#/unknown').page, 'database');
console.log('PASS configuration and workbench routes, old bookmarks, resource selection, parameters, and database switching');
