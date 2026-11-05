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
const { readRoute, setRouteParams, navigate } = sandbox.exports;
assert.equal(readRoute().page, 'sql-workbench');
// App initialization writes the database parameter and canonicalizes old links.
setRouteParams({ database: 'default' }, true);
assert.equal(location.hash, '#/sql-workbench?database=default&example=3&identity=175&limit=50');
assert.equal(readRoute().params.get('example'), '3');
navigate('sql-workbench', undefined, { database: 'other' });
assert.equal(location.hash, '#/sql-workbench?database=other');
assert.equal(readRoute('#/sql-workbench?database=default').page, 'sql-workbench');
assert.equal(readRoute('#/knowledge-workbench?database=default').page, 'knowledge-workbench');
console.log('PASS SQL workbench canonical route, old bookmarks, parameters, and database switching');
