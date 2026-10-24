const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const script = path.join(__dirname, '../datascope/views/sql.js');
const { fill, literal, validate } = require(script);
const variable = (type, value) => ({ type, default: value });

test('SQL literals preserve values and cannot break out of strings', () => {
  assert.equal(literal(variable('string', "O'Brien'; --")), "'O''Brien''; --'");
  assert.equal(literal(variable('integer', -12)), '(-12)');
  assert.equal(literal(variable('number', -1.25)), '(-1.25)');
  assert.equal(literal(variable('boolean', true)), '1');
  assert.equal(literal(variable('boolean', false)), '0');
  assert.throws(() => literal(variable('string', '\0')), /NUL/);
  for (const value of ['1; DROP TABLE t', 'Infinity', '0x10', 'NaN']) {
    assert.throws(() => literal(variable('number', 0), value));
  }
  assert.throws(() => literal(variable('integer', 0), '9007199254740992'));
});

test('dates reject normalization, impossible dates, timestamps and whitespace', () => {
  assert.equal(literal(variable('date', '2024-02-29')), "'2024-02-29'");
  for (const value of ['2026-02-29', '2026-04-31', '2026-13-01', '2026-1-01', '2026-10-20T00:00:00Z', ' 2026-10-20']) {
    assert.throws(() => literal(variable('date', value)));
  }
});

test('all defaults are validated before running, including unused definitions', () => {
  for (const [type, value] of [['string', 12], ['string', null], ['number', '12'], ['integer', 1.5], ['boolean', 'true'], ['date', '2026-02-29']]) {
    assert.ok(validate({ sql: 'SELECT 1', variables: { unused: variable(type, value) } }));
  }
  for (const variables of [null, [], undefined]) assert.ok(validate({ sql: 'SELECT 1', variables }));
  assert.equal(validate({ sql: 'SELECT 1', variables: {} }), '');
});

test('placeholders inside every quote/comment style fail even if undeclared', () => {
  for (const segment of ["'{{x}}'", '"{{x}}"', '`{{x}}`', '[{{x}}]', '-- {{x}}\n', '/* {{x}} */', "'it''s {{x}}'"]) {
    for (const variables of [{}, { x: variable('string', 'ok') }]) {
      assert.throws(() => fill(`SELECT ${segment}`, variables), /引号或注释/);
    }
  }
  assert.throws(() => fill('SELECT {{missing}}', {}), /未声明/);
  assert.throws(() => fill('SELECT {{Bad}}', {}), /无法识别/);
});

test('default filling, negative adjacency, options and prototype names work', () => {
  const variables = { name: variable('string', "D'Angelo"), n: variable('integer', -1), constructor: variable('string', 'ok') };
  const result = fill('SELECT {{name}}, 1-{{n}}, {{constructor}}', variables);
  assert.equal(result.sql, "SELECT 'D''Angelo', 1-(-1), 'ok'");
  assert.deepEqual(result.fieldErrors, {});
  assert.equal(fill('SELECT {{name}}', variables, { name: '' }).sql, "SELECT 'D''Angelo'");
  const def = { ...variable('integer', 1), options: [1, { value: 2, label: 'Two' }] };
  assert.equal(literal(def, '2'), '2');
  assert.throws(() => literal(def, '3'), /可选值/);
});

// A small DOM/host double exercises actual initialization and pending query handling.
function host() {
  class Element {
    constructor(tag) { this.tag = tag; this.children = []; this.listeners = {}; this.classList = { add() {} }; }
    append(...children) { this.children.push(...children); }
    replaceChildren(...children) { this.children = children; }
    setAttribute() {}
    addEventListener(name, fn) { this.listeners[name] = fn; }
    get childElementCount() { return this.children.length; }
  }
  const app = new Element('main');
  let receive;
  const calls = [];
  vm.runInNewContext(fs.readFileSync(script, 'utf8'), {
    document: { getElementById: () => app, createElement: tag => new Element(tag), documentElement: { lang: 'zh-CN' } },
    window: { viewHost: { onData: fn => { receive = fn; }, query: (name, params) => new Promise((resolve, reject) => calls.push({ name, params, resolve, reject })) } },
  });
  return { app, calls, receive: (doc, queryable = true) => receive(doc, {}, { queryable }) };
}
function text(node) { return [node.textContent || '', ...node.children.map(text)].join(' '); }
const flush = () => new Promise(resolve => setImmediate(resolve));
const doc = n => ({ sql: 'SELECT {{n}} AS value', variables: { n: variable('integer', n) }, maxRows: 50 });

test('host auto-runs defaults, sends only query SQL/row limit and does not persist edits', async () => {
  const h = host();
  const payload = doc(12);
  h.receive(payload);
  assert.equal(h.calls.length, 1);
  assert.equal(h.calls[0].name, 'run');
  assert.deepEqual(JSON.parse(JSON.stringify(h.calls[0].params)), { sql: 'SELECT 12 AS value', max_rows: 50 });
  h.calls[0].resolve({ data: { columns: ['value'], rows: [{ value: 12 }], row_count: 1 } });
  await flush();
  const form = h.app.children[1];
  const input = form.children[0].children[0].children[1];
  input.value = '-3'; input.listeners.input();
  form.listeners.submit({ preventDefault() {} });
  assert.equal(h.calls[1].params.sql, 'SELECT (-3) AS value');
  assert.equal(payload.variables.n.default, 12);
  const reopened = host(); reopened.receive(payload);
  assert.equal(reopened.calls[0].params.sql, 'SELECT 12 AS value');
});

test('invalid declarations or placeholders never invoke the host query', () => {
  for (const payload of [{ sql: 'SELECT {{x}}', variables: {} }, { sql: "SELECT '{{x}}'", variables: { x: variable('string', 'ok') } }, { sql: 'SELECT 1', variables: { x: variable('date', '2026-02-29') } }]) {
    const h = host(); h.receive(payload);
    assert.equal(h.calls.length, 0);
  }
});

test('old responses cannot populate a new invalid view; queryability restoration runs', async () => {
  const h = host(); h.receive(doc(1));
  h.receive({ sql: 'SELECT 1', variables: { x: variable('integer', 'bad') } });
  h.calls[0].resolve({ data: { rows: [{ value: 'STALE' }] } });
  await flush();
  assert.doesNotMatch(text(h.app), /STALE/);
  const offline = host(); offline.receive(doc(2), false);
  assert.equal(offline.calls.length, 0);
  offline.receive(doc(2), true);
  assert.equal(offline.calls.length, 1);
});

test('invalid edits invalidate an in-flight response without issuing another query', async () => {
  const h = host(); h.receive(doc(1));
  const form = h.app.children[1];
  const input = form.children[0].children[0].children[1];
  input.value = 'invalid'; input.listeners.input();
  form.listeners.submit({ preventDefault() {} });
  assert.equal(h.calls.length, 1);
  h.calls[0].resolve({ data: { rows: [{ value: 'STALE' }] } });
  await flush();
  assert.doesNotMatch(text(h.app), /STALE/);
  assert.match(text(h.app), /请修正/);
});

test('manifest routes exclusively through the permission-rewriting MCP tool', () => {
  const manifest = JSON.parse(fs.readFileSync(path.join(__dirname, '../datascope/view.json')));
  const run = manifest.views.sql.queries.run;
  assert.equal(run.adapter, 'mcp'); assert.equal(run.component, 'main'); assert.equal(run.toolName, 'query');
  assert.deepEqual(Object.keys(run.paramsSchema.properties).sort(), ['max_rows', 'sql']);
  assert.equal(run.paramsSchema.additionalProperties, false);
  for (const file of [manifest.views.sql.entry, ...manifest.views.sql.assets]) {
    assert.ok(fs.existsSync(path.join(__dirname, '../datascope', file)));
  }
});
